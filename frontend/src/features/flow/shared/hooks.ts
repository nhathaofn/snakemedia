import { useCallback, useEffect, useState } from 'react';
import { create } from 'zustand';
import { FLOW_SHARED_REQUEST_REUSE_MS } from '../../../constants';
import { api, isTransportFailure } from '../../../services/api';
import { useWailsEvent } from '../../../hooks/useWailsEvent';
import { useAppStore } from '../../../stores';
import { AppEvent, FlowImageProjectMode } from '../../../types';
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

const sharedRequests = new WeakMap<
  () => Promise<ApiResponse<unknown>>,
  { at: number; promise: Promise<ApiResponse<unknown>> }
>();

/**
 * Runs fetch, sharing one in-flight or recent successful call between the
 * panels that ask for the same thing. Failures are never reused.
 */
export function sharedRequest<T>(fetch: () => Promise<ApiResponse<T>>): Promise<ApiResponse<T>> {
  const key = fetch as () => Promise<ApiResponse<unknown>>;
  const cached = sharedRequests.get(key);
  if (cached && Date.now() - cached.at < FLOW_SHARED_REQUEST_REUSE_MS) {
    return cached.promise as Promise<ApiResponse<T>>;
  }
  const promise = fetch().then((res) => {
    if (!res.success) sharedRequests.delete(key);
    return res;
  });
  sharedRequests.set(key, { at: Date.now(), promise });
  return promise;
}

/**
 * Number of connected accounts that take turns generating (0 until loaded). The
 * backend decides which accounts count; the number is read again whenever an
 * account changes, so signing in elsewhere shows up here without a remount.
 */
export function useRotationCount(): number {
  const [count, setCount] = useState(0);
  const apply = useCallback((res: ApiResponse<number>) => {
    if (res.success && typeof res.data === 'number') setCount(res.data);
  }, []);
  useEffect(() => {
    let active = true;
    void sharedRequest(api.getActiveRotationCount).then((res) => {
      if (active) apply(res);
    });
    return () => {
      active = false;
    };
  }, [apply]);
  const refresh = useCallback(() => {
    void api.getActiveRotationCount().then(apply);
  }, [apply]);
  useWailsEvent(AppEvent.AccountsUpdated, refresh);
  return count;
}

/** Loads a studio's model catalogue into its store on mount. */
export function useModelCatalog<T>(
  fetchModels: () => Promise<ApiResponse<T[]>>,
  setModels: (models: T[]) => void,
): { loading: boolean; error: string | null } {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void sharedRequest(fetchModels).then((res) => {
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

export interface GenerationState<TItem> {
  prompt: string;
  setPrompt: (prompt: string) => void;
  isGenerating: boolean;
  activeGenerations: number;
  beginGeneration: () => void;
  finishGeneration: () => void;
  addItems: (items: TItem[]) => void;
}

/** A studio store read at call time, so a long generation sees the latest state. */
export interface GenerationStore<TItem> {
  getState: () => GenerationState<TItem>;
}

export interface GenerationLifecycle<TItem> {
  onStart?: () => void;
  onSuccess?: (items: TItem[]) => void;
  onFailure?: (message: string, promptRestored: boolean) => void;
  onUncertainFailure?: (message: string, promptRestored: boolean) => void;
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
  let responseReceived = false;
  let thrown = false;
  let failureMessage = 'Không rõ lỗi.';
  try {
    lifecycle.onStart?.();
    const res = await request();
    // A bridge failure is not an answer from Go: the job may already be running.
    responseReceived = !isTransportFailure(res);
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
    thrown = true;
    throw error;
  } finally {
    const latest = store.getState();
    const promptRestored = !accepted && latest.prompt === '' && latest.activeGenerations === 1;
    try {
      if (!accepted) {
        const failed =
          thrown || !responseReceived ? lifecycle.onUncertainFailure : lifecycle.onFailure;
        failed?.(failureMessage, promptRestored);
      }
    } finally {
      const current = store.getState();
      current.finishGeneration();
      if (promptRestored && current.prompt === '') current.setPrompt(submitted);
    }
  }
}

/** Shown when a generation may have reached Flow although no reply came back. */
export const UNCERTAIN_RETRY_HINT =
  'Yêu cầu có thể đã tới Flow; gửi lại đúng nội dung này để đối soát thay vì tạo job mới.';

/** Request IDs this process has sent and not yet settled. */
const inFlightRequestIDs = new Set<string>();

/**
 * Keeps one Flow request ID for identical unresolved submissions across retries
 * and restarts, so an attempt whose outcome is unknown is reconciled instead of
 * billed twice. Sending the same content again while that ID is still in
 * flight is a deliberate new generation and gets its own, unpersisted ID.
 */
export async function beginGenerationAttempt(
  kind: 'image' | 'video',
  request: unknown,
): Promise<{ requestId: string; settle: (pending: boolean) => void }> {
  void pruneGenerationAttempts();
  const serialized = JSON.stringify(request);
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(serialized));
  const fingerprint = Array.from(new Uint8Array(digest), (value) =>
    value.toString(16).padStart(2, '0'),
  ).join('');
  const key = `flow:generation:${kind}:${fingerprint}`;
  const savedIDs = generationRequestIDs();
  let requestId: string | null = null;
  try {
    requestId = localStorage.getItem(key);
  } catch {
    // Keep the ID for this process if host storage is disabled.
  }
  requestId ??= savedIDs.get(key) ?? null;
  if (requestId !== null && inFlightRequestIDs.has(requestId)) {
    const fresh = crypto.randomUUID();
    inFlightRequestIDs.add(fresh);
    return { requestId: fresh, settle: () => inFlightRequestIDs.delete(fresh) };
  }
  requestId ??= crypto.randomUUID();
  const sentID = requestId;
  inFlightRequestIDs.add(sentID);
  savedIDs.set(key, requestId);
  try {
    localStorage.setItem(key, requestId);
  } catch {
    // The active request still has a stable ID.
  }
  return {
    requestId,
    settle: (pending) => {
      inFlightRequestIDs.delete(sentID);
      if (pending) return;
      try {
        if (localStorage.getItem(key) === requestId) localStorage.removeItem(key);
      } catch {
        // Storage may be disabled by the host webview.
      }
      if (savedIDs.get(key) === requestId) savedIDs.delete(key);
    },
  };
}

const generationRequestIDs = (): Map<string, string> => {
  const root = globalThis as typeof globalThis & {
    __flowGenerationRequestIDs?: Map<string, string>;
  };
  root.__flowGenerationRequestIDs ??= new Map();
  return root.__flowGenerationRequestIDs;
};

/** Clears the browser-side key after a pending job is explicitly resumed to completion. */
export function clearGenerationAttempt(requestId: string): void {
  const savedIDs = generationRequestIDs();
  for (const [key, value] of savedIDs) {
    if (value === requestId) savedIDs.delete(key);
  }
  try {
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const key = localStorage.key(i);
      if (key?.startsWith('flow:generation:') && localStorage.getItem(key) === requestId) {
        localStorage.removeItem(key);
      }
    }
  } catch {
    // Storage may be disabled by the host webview.
  }
}

/**
 * Saved attempt keys whose request ID is no longer worth keeping: neither a
 * job the backend still reports pending nor a request this process is sending.
 */
export function staleGenerationKeys(
  saved: Iterable<readonly [string, string]>,
  keep: ReadonlySet<string>,
): string[] {
  const stale: string[] = [];
  for (const [key, requestId] of saved) {
    if (key.startsWith('flow:generation:') && !keep.has(requestId)) stale.push(key);
  }
  return stale;
}

let attemptsPruned = false;

/**
 * Once per session, drops saved attempt IDs the backend no longer reports as
 * pending (settled, expired or never received), so keys of jobs that were never
 * resumed do not pile up in storage. Nothing is dropped when a list fails.
 */
async function pruneGenerationAttempts(): Promise<void> {
  if (attemptsPruned) return;
  attemptsPruned = true;
  const lists = await Promise.all([
    api.listPendingImageGenerations(FlowImageProjectMode.Single),
    api.listPendingImageGenerations(FlowImageProjectMode.All),
    api.listPendingVideoGenerations(),
  ]);
  if (lists.some((res) => !res.success)) return;
  const keep = new Set(inFlightRequestIDs);
  for (const res of lists) for (const item of res.data ?? []) if (item.jobId) keep.add(item.jobId);
  const savedIDs = generationRequestIDs();
  for (const key of staleGenerationKeys(savedIDs, keep)) savedIDs.delete(key);
  try {
    const stored: [string, string][] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i);
      if (key) stored.push([key, localStorage.getItem(key) ?? '']);
    }
    for (const key of staleGenerationKeys(stored, keep)) localStorage.removeItem(key);
  } catch {
    // Storage may be disabled by the host webview.
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
