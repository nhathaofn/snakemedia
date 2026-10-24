import { create } from 'zustand';
import type { FlowEditModel, GeneratedVideoItem, ImportVideoResult } from '../../../../types';
import { createStudioSlice } from '../../shared';
import type { StudioSlice } from '../../shared';

/** Shortest part of the source the trim handles may leave, in seconds. */
export const MIN_TRIM_SECONDS = 0.5;

/**
 * Edit video state. As in Flow's editor there is no ratio, quality or count to
 * pick: one video is made and the backend takes ratio and quality from the source.
 */
export interface FlowEditState extends StudioSlice<FlowEditModel, GeneratedVideoItem> {
  /** Video imported into the media folder, or null before one is picked. */
  source: ImportVideoResult | null;
  setSource: (source: ImportVideoResult) => void;
  /** Whether the source video is currently being loaded/decoded or uploaded to Flow. */
  isSourceLoading: boolean;
  /** Loading message describing the current step. */
  sourceLoadingMessage: string;
  /** Error message if loading, decoding or uploading the source video failed. */
  sourceError: string | null;
  /** Media ID returned by Google Flow Scotty uploader once uploaded. */
  uploadedMediaId: string | null;
  setSourceLoading: (loading: boolean, message?: string) => void;
  setSourceError: (error: string | null) => void;
  setUploadedMedia: (mediaId: string, width?: number, height?: number) => void;
  /** Source length in seconds once the player has read it, otherwise 0. */
  sourceDuration: number;
  /** Source frame size in pixels for display, 0 until the player has read it. */
  sourceWidth: number;
  sourceHeight: number;
  /** Records what the player read from the source and resets the trim. */
  setSourceMetadata: (seconds: number, width: number, height: number) => void;
  trimStart: number;
  trimEnd: number;
  /** Moves the trim handles, keeping them inside the video and the model's input limit. */
  setTrim: (start: number, end: number) => void;
}

/** Trim handles clamped to the video and to the longest input the model reads. */
export function clampTrim(start: number, end: number, duration: number, maxInput: number) {
  if (duration <= 0) return { trimStart: 0, trimEnd: 0 };
  const window = maxInput > 0 ? Math.min(maxInput, duration) : duration;
  const s = Math.min(Math.max(0, start), Math.max(0, duration - MIN_TRIM_SECONDS));
  const e = Math.min(Math.max(end, s + MIN_TRIM_SECONDS), s + window, duration);
  return { trimStart: s, trimEnd: e };
}

const maxInputOf = (state: FlowEditState) =>
  state.models.find((m) => m.id === state.selectedModel)?.maxInputSeconds ?? 0;

const DEFAULT_SOURCE_LOADING_MESSAGE = 'Đang tải video lên Google Flow và xử lý thông số...';

export const useFlowEditStore = create<FlowEditState>()((set) => ({
  ...createStudioSlice<FlowEditModel, GeneratedVideoItem, FlowEditState>(set, (model, state) =>
    clampTrim(state.trimStart, state.trimEnd, state.sourceDuration, model?.maxInputSeconds ?? 0),
  ),
  source: null,
  isSourceLoading: false,
  sourceLoadingMessage: DEFAULT_SOURCE_LOADING_MESSAGE,
  sourceError: null,
  uploadedMediaId: null,
  setSource: (source) =>
    set({
      source,
      sourceDuration: 0,
      sourceWidth: 0,
      sourceHeight: 0,
      trimStart: 0,
      trimEnd: 0,
      isSourceLoading: true,
      sourceLoadingMessage: DEFAULT_SOURCE_LOADING_MESSAGE,
      sourceError: null,
      uploadedMediaId: null,
    }),
  setSourceLoading: (isSourceLoading, message) =>
    set({
      isSourceLoading,
      sourceLoadingMessage: message ?? DEFAULT_SOURCE_LOADING_MESSAGE,
    }),
  setSourceError: (sourceError) => set({ sourceError, isSourceLoading: false }),
  setUploadedMedia: (mediaId, width, height) =>
    set((state) => ({
      uploadedMediaId: mediaId,
      sourceWidth: width && width > 0 ? width : state.sourceWidth,
      sourceHeight: height && height > 0 ? height : state.sourceHeight,
      isSourceLoading: state.sourceDuration <= 0,
    })),
  sourceDuration: 0,
  sourceWidth: 0,
  sourceHeight: 0,
  // A new video starts with the longest part from its beginning the model reads.
  setSourceMetadata: (seconds, width, height) =>
    set((state) => ({
      sourceDuration: seconds,
      sourceWidth: state.sourceWidth > 0 ? state.sourceWidth : width,
      sourceHeight: state.sourceHeight > 0 ? state.sourceHeight : height,
      isSourceLoading: !state.uploadedMediaId,
      sourceError: null,
      ...clampTrim(0, seconds, seconds, maxInputOf(state)),
    })),
  trimStart: 0,
  trimEnd: 0,
  setTrim: (start, end) =>
    set((state) => clampTrim(start, end, state.sourceDuration, maxInputOf(state))),
}));

/**
 * Trim to send, in milliseconds: none (0, 0) when the whole video is used, so
 * Flow receives it exactly as its own uploader would send an untrimmed file.
 */
export function trimRequest(
  state: Pick<FlowEditState, 'sourceDuration' | 'trimStart' | 'trimEnd'>,
) {
  const whole = state.trimStart <= 0 && state.trimEnd >= state.sourceDuration;
  if (state.sourceDuration <= 0 || whole) return { trimStartMs: 0, trimEndMs: 0 };
  // The length is floored so rounding never pushes it past the model's limit.
  const trimStartMs = Math.round(state.trimStart * 1000);
  return {
    trimStartMs,
    trimEndMs: trimStartMs + Math.floor((state.trimEnd - state.trimStart) * 1000 + 1e-6),
  };
}
