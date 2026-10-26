import { create } from 'zustand';
import { FlowImageQuality } from '../../../../types';
import type {
  FlowModel,
  FlowImageQuality as FlowImageQualityType,
  GeneratedImageItem,
} from '../../../../types';
import { createReferenceSlice, createStudioSlice } from '../../shared';
import type { ReferenceSlice, StudioSlice } from '../../shared';

export const MAX_ALL_IMAGE_THREADS = 10;
export const ALL_IMAGE_WAIT_MODES = [
  { value: 'immediate', label: 'Ngay lập tức', minSeconds: 0, maxSeconds: 0 },
  { value: '5-10s', label: '5-10s', minSeconds: 5, maxSeconds: 10 },
  { value: '10-20s', label: '10-20s', minSeconds: 10, maxSeconds: 20 },
  { value: '20-30s', label: '20-30s', minSeconds: 20, maxSeconds: 30 },
  { value: '30-60s', label: '30-60s', minSeconds: 30, maxSeconds: 60 },
  { value: '60-90s', label: '60-90s', minSeconds: 60, maxSeconds: 90 },
  { value: '90-120s', label: '90-120s', minSeconds: 90, maxSeconds: 120 },
] as const;

export type AllImageWaitMode = (typeof ALL_IMAGE_WAIT_MODES)[number]['value'];

export const ALL_IMAGE_REFERENCE_MODES = [
  { value: 'one-for-all', label: '1 cho tất cả' },
  { value: 'sequential', label: 'Chạy tuần tự' },
] as const;

export type AllImageReferenceMode = (typeof ALL_IMAGE_REFERENCE_MODES)[number]['value'];

export type QueuedImagePromptStatus =
  'queued' | 'processing' | 'completed' | 'failed' | 'save-failed';

export interface QueuedImageReference {
  base64: string;
  mimeType: string;
  name: string;
}

export interface QueuedImagePrompt {
  id: string;
  batchId: string;
  prompt: string;
  modelId: string;
  aspectRatio: string;
  quality: FlowImageQualityType;
  count: number;
  references: QueuedImageReference[];
  status: QueuedImagePromptStatus;
  results: GeneratedImageItem[];
  outputDirectory?: string;
  savedLocalURLs: string[];
  error?: string;
  saveError?: string;
  /**
   * Flow request ID of the last attempt whose outcome is unknown (no reply or
   * still pending). The next run reuses it so Flow reconciles that job instead
   * of billing a new one; cleared once Flow answers or the request changes.
   */
  requestId?: string;
  /**
   * Read back from the backend after a restart. Its references are gone, so it
   * is only ever checked again by request ID, never sent as a new request.
   */
  restored?: boolean;
}

export interface FlowImageState extends StudioSlice<FlowModel, GeneratedImageItem>, ReferenceSlice {
  allAspectRatio: string;
  setAllAspectRatio: (ratio: string) => void;
  allImageQuality: FlowImageQualityType;
  setAllImageQuality: (quality: FlowImageQualityType) => void;
  allImageCountPerPrompt: number;
  setAllImageCountPerPrompt: (count: number) => void;
  allReferenceMode: AllImageReferenceMode;
  setAllReferenceMode: (mode: AllImageReferenceMode) => void;
  allThreads: number;
  setAllThreads: (threads: number) => void;
  allWaitMode: AllImageWaitMode;
  setAllWaitMode: (mode: AllImageWaitMode) => void;
  hideGeminiLogo: boolean;
  setHideGeminiLogo: (hidden: boolean) => void;
  imageOutputDirectory: string;
  setImageOutputDirectory: (directory: string) => void;
  allPrompts: QueuedImagePrompt[];
  addAllPrompts: (
    prompts: string[],
    modelId: string,
    aspectRatio: string,
    quality: FlowImageQualityType,
    count: number,
    references: QueuedImageReference[],
  ) => void;
  /** Adds unfinished All jobs saved by the backend as failed tasks to check again. */
  restoreAllPrompts: (jobs: GeneratedImageItem[], quality: FlowImageQualityType) => void;
  updateAllPrompt: (
    id: string,
    update: Partial<
      Pick<
        QueuedImagePrompt,
        | 'status'
        | 'results'
        | 'references'
        | 'error'
        | 'outputDirectory'
        | 'savedLocalURLs'
        | 'saveError'
        | 'requestId'
        | 'restored'
      >
    >,
  ) => void;
  removeAllPrompts: (ids: string[]) => void;
  allProcessing: boolean;
  setAllProcessing: (processing: boolean) => void;
  allPaused: boolean;
  setAllPaused: (paused: boolean) => void;
  allStopRequested: boolean;
  setAllStopRequested: (requested: boolean) => void;
  previewImage: GeneratedImageItem | null;
  setPreviewImage: (image: GeneratedImageItem | null) => void;
}

export const useFlowImageStore = create<FlowImageState>()((set) => ({
  ...createStudioSlice<FlowModel, GeneratedImageItem, FlowImageState>(set),
  ...createReferenceSlice<FlowImageState>(set),
  // Filled from StudioConfig.defaultBatchRatio once the studio config loads.
  allAspectRatio: '',
  setAllAspectRatio: (allAspectRatio) => set({ allAspectRatio }),
  allImageQuality: FlowImageQuality.OneK,
  setAllImageQuality: (allImageQuality) => set({ allImageQuality }),
  allImageCountPerPrompt: 1,
  setAllImageCountPerPrompt: (allImageCountPerPrompt) => {
    if (Number.isInteger(allImageCountPerPrompt) && allImageCountPerPrompt >= 1) {
      set({ allImageCountPerPrompt });
    }
  },
  allReferenceMode: ALL_IMAGE_REFERENCE_MODES[0].value,
  setAllReferenceMode: (allReferenceMode) => set({ allReferenceMode }),
  allThreads: 1,
  setAllThreads: (allThreads) => {
    if (Number.isInteger(allThreads) && allThreads >= 1 && allThreads <= MAX_ALL_IMAGE_THREADS) {
      set({ allThreads });
    }
  },
  allWaitMode: ALL_IMAGE_WAIT_MODES[1].value,
  setAllWaitMode: (allWaitMode) => set({ allWaitMode }),
  hideGeminiLogo: true,
  setHideGeminiLogo: (hideGeminiLogo) => set({ hideGeminiLogo }),
  imageOutputDirectory: '',
  setImageOutputDirectory: (imageOutputDirectory) => set({ imageOutputDirectory }),
  allPrompts: [],
  addAllPrompts: (prompts, modelId, aspectRatio, quality, count, references) => {
    const batchId = crypto.randomUUID();
    set((state) => ({
      allPrompts: [
        ...state.allPrompts,
        ...prompts.map((prompt) => ({
          id: crypto.randomUUID(),
          batchId,
          prompt,
          modelId,
          aspectRatio,
          quality,
          count,
          references,
          status: 'queued' as const,
          results: [],
          savedLocalURLs: [],
        })),
      ],
    }));
  },
  restoreAllPrompts: (jobs, quality) =>
    set((state) => {
      const known = new Set(state.allPrompts.map((task) => task.requestId));
      const byJob = new Map<string, GeneratedImageItem[]>();
      for (const item of jobs) {
        if (!item.jobId || known.has(item.jobId)) continue;
        byJob.set(item.jobId, [...(byJob.get(item.jobId) ?? []), item]);
      }
      if (byJob.size === 0) return {};
      const batchId = crypto.randomUUID();
      const restored: QueuedImagePrompt[] = [...byJob].map(([jobId, items]) => ({
        id: crypto.randomUUID(),
        batchId,
        prompt: items[0].prompt,
        modelId: items[0].model,
        aspectRatio: items[0].aspectRatio,
        quality,
        count: items.length,
        references: [],
        status: 'failed',
        results: items,
        savedLocalURLs: [],
        error: items[0].error ?? 'Flow chưa xác nhận kết quả của job này.',
        requestId: jobId,
        restored: true,
      }));
      return { allPrompts: [...restored, ...state.allPrompts] };
    }),
  updateAllPrompt: (id, update) =>
    set((state) => ({
      allPrompts: state.allPrompts.map((item) => {
        if (item.id !== id) return item;
        // New references make a new request: an old request ID would be rejected,
        // and a restored job becomes an ordinary task to send.
        const changed = update.references !== undefined && update.requestId === undefined;
        return {
          ...item,
          ...update,
          ...(changed ? { requestId: undefined, restored: false } : {}),
        };
      }),
    })),
  removeAllPrompts: (ids) =>
    set((state) =>
      state.allProcessing
        ? {}
        : {
            allPrompts: state.allPrompts.filter(
              (item) => item.status === 'processing' || !ids.includes(item.id),
            ),
          },
    ),
  allProcessing: false,
  setAllProcessing: (allProcessing) => set({ allProcessing }),
  allPaused: false,
  setAllPaused: (allPaused) => set({ allPaused }),
  allStopRequested: false,
  setAllStopRequested: (allStopRequested) => set({ allStopRequested }),
  previewImage: null,
  setPreviewImage: (previewImage) => set({ previewImage }),
}));
