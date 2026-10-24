import { create } from 'zustand';
import type {
  EditVideoRequest,
  FlowEditModel,
  GeneratedVideoItem,
  ImportVideoResult,
  UploadedSourceVideo,
} from '../../../../types';
import { createStudioSlice } from '../../shared';
import type { StudioSlice } from '../../shared';

/**
 * Edit video state. As in Flow's editor there is no ratio, quality or count to
 * pick: one video is made and the backend takes ratio and quality from the source.
 */
export interface FlowEditState extends StudioSlice<FlowEditModel, GeneratedVideoItem> {
  /** Video imported into the media folder, or null before one is picked. */
  source: ImportVideoResult | null;
  editAttempt: { key: string; id: string } | null;
  requestIDFor: (request: EditVideoRequest) => string;
  acceptAttempt: (id: string) => void;
  setSource: (source: ImportVideoResult) => number;
  sourceRevision: number;
  /** Whether the source video is currently being loaded/decoded or uploaded to Flow. */
  isSourceLoading: boolean;
  /** Loading message describing the current step. */
  sourceLoadingMessage: string;
  /** Error message if loading, decoding or uploading the source video failed. */
  sourceError: string | null;
  /** Opaque backend reference, published only after Flow finishes preparing the source. */
  uploadedSourceID: string | null;
  /** Failed results dismissed during this app session. */
  dismissedItemIds: string[];
  setSourceLoading: (loading: boolean, message?: string) => void;
  setSourceError: (error: string | null, revision?: number) => void;
  setUploadedSource: (source: UploadedSourceVideo, revision: number) => void;
  /** Source length on Flow's prepared timeline; local preview metadata is provisional. */
  sourceDuration: number;
  /** Source frame size in pixels for display, 0 until the player has read it. */
  sourceWidth: number;
  sourceHeight: number;
  /** Records what the player read from the source and resets the trim. */
  setSourceMetadata: (seconds: number, width: number, height: number, revision?: number) => void;
  upsertItems: (items: GeneratedVideoItem[]) => void;
  /** Hides a saved result from the gallery without deleting its backend history. */
  dismissItem: (id: string) => void;
  minTrimSeconds: number;
  trimStepSeconds: number;
  trimStart: number;
  trimEnd: number;
  /** Moves the trim handles, keeping them inside the video and the model's input limit. */
  setTrim: (start: number, end: number) => void;
}

/** Trim handles clamped to the video and to the longest input the model reads. */
export function clampTrim(
  start: number,
  end: number,
  duration: number,
  maxInput: number,
  minimumSeconds = 0,
) {
  if (duration <= 0) return { trimStart: 0, trimEnd: 0 };
  const window = maxInput > 0 ? Math.min(maxInput, duration) : duration;
  const minimum = Math.min(minimumSeconds, window);
  const s = Math.min(Math.max(0, start), Math.max(0, duration - minimum));
  const e = Math.min(Math.max(end, s + minimum), s + window, duration);
  return { trimStart: s, trimEnd: e };
}

const maxInputOf = (state: FlowEditState) =>
  state.models.find((m) => m.id === state.selectedModel)?.maxInputSeconds ?? 0;

const DEFAULT_SOURCE_LOADING_MESSAGE = 'Đang tải video lên Google Flow và xử lý thông số...';

function itemKey(item: Pick<GeneratedVideoItem, 'id' | 'jobId'>): string {
  return item.jobId || item.id;
}

export const useFlowEditStore = create<FlowEditState>()((set, get) => ({
  ...createStudioSlice<FlowEditModel, GeneratedVideoItem, FlowEditState>(set, (model, state) =>
    clampTrim(
      state.trimStart,
      state.trimEnd,
      state.sourceDuration,
      model?.maxInputSeconds ?? 0,
      state.minTrimSeconds,
    ),
  ),
  upsertItems: (items) =>
    set((state) => {
      const dismissed = new Set(state.dismissedItemIds);
      const incoming = items.filter(
        (item) =>
          !dismissed.has(itemKey(item)) &&
          !(
            item.pending &&
            item.jobId &&
            state.items.some((saved) => saved.jobId === item.jobId && !saved.pending)
          ),
      );
      const jobs = new Set(incoming.map((item) => item.jobId).filter(Boolean));
      const ids = new Set(incoming.map((item) => item.id));
      return {
        items: [
          ...incoming,
          ...state.items.filter(
            (item) =>
              !dismissed.has(itemKey(item)) &&
              !ids.has(item.id) &&
              !(item.jobId && jobs.has(item.jobId)),
          ),
        ],
      };
    }),
  dismissItem: (id) => {
    if (!id) return;
    set((state) => ({
      dismissedItemIds: state.dismissedItemIds.includes(id)
        ? state.dismissedItemIds
        : [...state.dismissedItemIds, id],
      items: state.items.filter((item) => itemKey(item) !== id),
    }));
  },
  editAttempt: null,
  requestIDFor: (request) => {
    const key = JSON.stringify([
      request.sourceId,
      request.sourceUrl,
      request.modelId,
      request.prompt,
      request.trimStartMs,
      request.trimEndMs,
      request.projectId ?? '',
    ]);
    const previous = get().editAttempt;
    if (previous?.key === key) return previous.id;
    const id = crypto.randomUUID();
    set({ editAttempt: { key, id } });
    return id;
  },
  acceptAttempt: (id) =>
    set((state) => (state.editAttempt?.id === id ? { editAttempt: null } : {})),
  source: null,
  isSourceLoading: false,
  sourceLoadingMessage: DEFAULT_SOURCE_LOADING_MESSAGE,
  sourceError: null,
  uploadedSourceID: null,
  dismissedItemIds: [],
  sourceRevision: 0,
  setSource: (source) => {
    const revision = get().sourceRevision + 1;
    set({
      source,
      sourceRevision: revision,
      sourceDuration: 0,
      sourceWidth: 0,
      sourceHeight: 0,
      minTrimSeconds: 0,
      trimStepSeconds: 0,
      trimStart: 0,
      trimEnd: 0,
      isSourceLoading: true,
      sourceLoadingMessage: DEFAULT_SOURCE_LOADING_MESSAGE,
      sourceError: null,
      uploadedSourceID: null,
    });
    return revision;
  },
  setSourceLoading: (isSourceLoading, message) =>
    set({ isSourceLoading, sourceLoadingMessage: message ?? DEFAULT_SOURCE_LOADING_MESSAGE }),
  setSourceError: (sourceError, revision) =>
    set((state) =>
      revision !== undefined && revision !== state.sourceRevision
        ? {}
        : { sourceError, isSourceLoading: false },
    ),
  setUploadedSource: (uploaded, revision) => {
    if (revision !== get().sourceRevision) return;
    get().setModels(uploaded.models);
    set((state) => ({
      uploadedSourceID: uploaded.sourceId,
      minTrimSeconds: uploaded.minTrimSeconds,
      trimStepSeconds: uploaded.trimStepSeconds,
      sourceWidth: uploaded.width,
      sourceHeight: uploaded.height,
      sourceDuration: uploaded.durationSeconds,
      isSourceLoading: false,
      ...clampTrim(
        0,
        uploaded.durationSeconds,
        uploaded.durationSeconds,
        maxInputOf(state),
        uploaded.minTrimSeconds,
      ),
    }));
  },
  sourceDuration: 0,
  sourceWidth: 0,
  sourceHeight: 0,
  // A new video starts with the longest part from its beginning the model reads.
  setSourceMetadata: (seconds, width, height, revision) =>
    set((state) =>
      (revision !== undefined && revision !== state.sourceRevision) || state.uploadedSourceID
        ? {}
        : {
            sourceDuration: seconds,
            sourceWidth: state.sourceWidth > 0 ? state.sourceWidth : width,
            sourceHeight: state.sourceHeight > 0 ? state.sourceHeight : height,

            ...clampTrim(0, seconds, seconds, maxInputOf(state), state.minTrimSeconds),
          },
    ),
  minTrimSeconds: 0,
  trimStepSeconds: 0,
  trimStart: 0,
  trimEnd: 0,
  setTrim: (start, end) =>
    set((state) =>
      clampTrim(start, end, state.sourceDuration, maxInputOf(state), state.minTrimSeconds),
    ),
}));

/**
 * Always send the selected interval, including a whole short source.
 */
export function trimRequest(
  state: Pick<FlowEditState, 'sourceDuration' | 'trimStart' | 'trimEnd'>,
) {
  // The length is floored so rounding never pushes it past the model's limit.
  const trimStartMs = Math.round(state.trimStart * 1000);
  return {
    trimStartMs,
    trimEndMs: trimStartMs + Math.floor((state.trimEnd - state.trimStart) * 1000 + 1e-6),
  };
}
