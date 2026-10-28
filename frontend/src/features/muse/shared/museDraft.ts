import { useCallback } from 'react';
import { create } from 'zustand';
import type { MuseJobKind, MuseJobMode, MuseRefMode, MuseReference } from '../../../types';

/**
 * What one studio screen holds before it is sent: prompt and variants (Đơn
 * only), references, and the ratio and duration (video) asked of Muse: empty
 * or 0 leaves them to the prompt.
 */
export interface MuseDraft {
  prompt: string;
  variants: number;
  references: MuseReference[];
  ratio: string;
  duration: number;
  /** How a batch's own images go to its prompts; empty shares them. */
  refMode: MuseRefMode | '';
  /** Assets attached to every prompt (a fixed model or product). */
  assetIds: string[];
  /** Storyboard frames drawn before each video (0: none), and whether the video waits for review. */
  boardFrames: number;
  boardReview: boolean;
  /** A video whose movement every prompt recreates (v2v). */
  sourceVideo: MuseReference | null;
}

const EMPTY: MuseDraft = {
  prompt: '',
  variants: 1,
  references: [],
  ratio: '',
  duration: 0,
  refMode: '',
  assetIds: [],
  boardFrames: 0,
  boardReview: false,
  sourceVideo: null,
};

const useDrafts = create<{
  drafts: Record<string, MuseDraft>;
  patch: (key: string, change: Partial<MuseDraft>) => void;
}>((set) => ({
  drafts: {},
  patch: (key, change) =>
    set((state) => ({
      drafts: { ...state.drafts, [key]: { ...EMPTY, ...state.drafts[key], ...change } },
    })),
}));

/**
 * The draft of one kind and mode, shared by its tools panel and workspace and
 * kept while the user moves between pages.
 */
export function useMuseDraft(kind: MuseJobKind, mode: MuseJobMode) {
  const key = `${kind}:${mode}`;
  const draft = useDrafts((s) => s.drafts[key] ?? EMPTY);
  const patch = useDrafts((s) => s.patch);
  const update = useCallback((change: Partial<MuseDraft>) => patch(key, change), [patch, key]);
  return [draft, update] as const;
}
