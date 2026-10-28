import { create } from 'zustand';
import { api, unwrapList } from '../../../services';
import type { MuseAsset, MusePromptTemplate } from '../../../types';

interface MuseStudioState {
  assets: MuseAsset[];
  templates: MusePromptTemplate[];
  loaded: boolean;
  /** Why the last load failed; screens show it once instead of retrying. */
  loadError: string | null;
  /** Loads once for every screen that asks; a failed load is not retried until the app restarts. */
  ensure: () => void;
  /** Reloads the asset library and the saved prompts; errors are thrown to the caller. */
  load: () => Promise<void>;
}

// Set by the first ensure(): every field mounts it, but one load is enough.
let started = false;

/** The Muse asset library (Tư liệu) and saved prompts, shared by every Muse screen. */
export const useMuseStudioStore = create<MuseStudioState>((set, get) => ({
  assets: [],
  templates: [],
  loaded: false,
  loadError: null,
  ensure: () => {
    if (started) return;
    started = true;
    get()
      .load()
      .catch((cause: unknown) =>
        set({ loaded: true, loadError: cause instanceof Error ? cause.message : String(cause) }),
      );
  },
  load: async () => {
    const [assets, templates] = await Promise.all([
      api.listMuseAssets().then(unwrapList),
      api.listMuseTemplates().then(unwrapList),
    ]);
    set({ assets, templates, loaded: true, loadError: null });
  },
}));

/** The asset IDs that still exist: a draft may hold assets deleted since it chose them. */
export function liveAssetIds(ids: readonly string[]): string[] {
  const { assets } = useMuseStudioStore.getState();
  return ids.filter((id) => assets.some((a) => a.id === id));
}
