import { useCallback, useEffect, useState } from 'react';
import { create } from 'zustand';
import { api } from '../../../services/api';
import { useAppStore } from '../../../stores';
import type { ApiResponse, StudioConfig, ToastNotification } from '../../../types';

interface StudioConfigState {
  config: StudioConfig | null;
  error: string | null;
  load: () => Promise<void>;
}

/** Choices and limits the backend enforces (ratios, counts, reference rules), loaded once. */
const useStudioConfigStore = create<StudioConfigState>((set, get) => ({
  config: null,
  error: null,
  load: async () => {
    if (get().config) return;
    const res = await api.getStudioConfig();
    if (res.success && res.data) set({ config: res.data, error: null });
    else set({ error: res.error ?? 'Không tải được cấu hình studio.' });
  },
}));

export function useStudioConfig(): { config: StudioConfig | null; error: string | null } {
  const config = useStudioConfigStore((s) => s.config);
  const error = useStudioConfigStore((s) => s.error);
  const load = useStudioConfigStore((s) => s.load);
  useEffect(() => {
    void load();
  }, [load]);
  return { config, error };
}

/** Loads a studio's model catalogue into its store once per mount. */
export function useModelCatalog<T>(
  fetchModels: () => Promise<ApiResponse<T[]>>,
  setModels: (models: T[]) => void,
): { loading: boolean; error: string | null } {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void fetchModels().then((res) => {
      if (!active) return;
      if (res.success && res.data) {
        setModels(res.data);
        setError(null);
      } else {
        setModels([]);
        setError(res.error ?? 'Không tải được danh sách model.');
      }
      setLoading(false);
    });
    return () => {
      active = false;
    };
  }, [fetchModels, setModels]);

  return { loading, error };
}

interface GenerationState<TItem> {
  prompt: string;
  setPrompt: (prompt: string) => void;
  isGenerating: boolean;
  activeGenerations: number;
  beginGeneration: () => void;
  finishGeneration: () => void;
  addItems: (items: TItem[]) => void;
}

/** A studio store read at call time, so a long generation sees the latest state. */
interface GenerationStore<TItem> {
  getState: () => GenerationState<TItem>;
}

export interface GenerationLifecycle<TItem> {
  onStart?: () => void;
  onSuccess?: (items: TItem[]) => void;
  onFailure?: (message: string, promptRestored: boolean) => void;
}

type Notify = (toast: Omit<ToastNotification, 'id'>) => void;

/**
 * Runs one generation like a chat message: the prompt box clears as soon as the
 * request is sent; if it fails, the prompt comes back so it can be retried when
 * no other request is active and the user has not typed something new. Further
 * prompts can be submitted while earlier requests continue.
 * `noun` names the media in messages ("ảnh", "video").
 */
export async function runGeneration<TItem extends { error?: string; pending?: boolean }>(
  store: GenerationStore<TItem>,
  request: () => Promise<ApiResponse<TItem[]>>,
  noun: string,
  notify: Notify,
  lifecycle: GenerationLifecycle<TItem> = {},
): Promise<void> {
  const state = store.getState();
  const submitted = state.prompt;
  state.beginGeneration();
  state.setPrompt('');
  let accepted = false;
  let failureMessage = 'Không rõ lỗi.';
  try {
    lifecycle.onStart?.();
    const res = await request();
    if (!res.success || !res.data) {
      failureMessage = res.error ?? failureMessage;
      notify({
        type: 'error',
        title: `Không tạo được ${noun}`,
        message: failureMessage,
      });
      return;
    }
    accepted = true;
    store.getState().addItems(res.data);
    lifecycle.onSuccess?.(res.data);
    const failed = res.data.filter((item) => item.error && !item.pending).length;
    if (failed > 0) {
      notify({
        type: 'warning',
        title: `${failed}/${res.data.length} ${noun} không hiển thị được`,
        message: 'Xem lý do trên từng thẻ.',
      });
    }
  } catch (error) {
    failureMessage = error instanceof Error ? error.message : String(error);
    throw error;
  } finally {
    const latest = store.getState();
    const promptRestored = !accepted && latest.prompt === '' && latest.activeGenerations === 1;
    try {
      if (!accepted) lifecycle.onFailure?.(failureMessage, promptRestored);
    } finally {
      const current = store.getState();
      current.finishGeneration();
      if (promptRestored && current.prompt === '') current.setPrompt(submitted);
    }
  }
}

/** Binds runGeneration to a studio store and the app's toasts. */
export function useGeneration<TItem extends { error?: string; pending?: boolean }>(
  store: GenerationStore<TItem>,
  noun: string,
) {
  const addToast = useAppStore((s) => s.addToast);
  return useCallback(
    (request: () => Promise<ApiResponse<TItem[]>>, lifecycle?: GenerationLifecycle<TItem>) =>
      runGeneration(store, request, noun, addToast, lifecycle),
    [store, noun, addToast],
  );
}

/** Saves a downloaded image or video into a folder the user picks. */
export function useSaveMedia() {
  const addToast = useAppStore((s) => s.addToast);
  return useCallback(
    async (localUrl: string) => {
      const res = await api.saveMedia(localUrl);
      if (!res.success || !res.data) {
        addToast({
          type: 'error',
          title: 'Không lưu được tệp',
          message: res.error ?? 'Không rõ lỗi.',
        });
        return;
      }
      if (!res.data.canceled) {
        addToast({ type: 'success', title: 'Đã lưu', message: res.data.path });
      }
    },
    [addToast],
  );
}
