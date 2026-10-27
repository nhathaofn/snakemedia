import { create } from 'zustand';
import { FlowImageQuality } from '../../../../types';
import type {
  FlowModel,
  FlowImageQuality as FlowImageQualityType,
  FlowImageTaskReference,
  GeneratedImageItem,
} from '../../../../types';
import { createReferenceSlice, createStudioSlice } from '../../shared';
import type { ReferenceSlice, StudioSlice } from '../../shared';

export interface FlowImageState extends StudioSlice<FlowModel, GeneratedImageItem>, ReferenceSlice {
  allAspectRatio: string;
  setAllAspectRatio: (ratio: string) => void;
  allImageQuality: FlowImageQualityType;
  setAllImageQuality: (quality: FlowImageQualityType) => void;
  imageOutputDirectory: string;
  setImageOutputDirectory: (directory: string) => void;
  /** References (in the media store) given to prompts added to the All queue next. */
  queueReferences: FlowImageTaskReference[];
  addQueueReferences: (references: FlowImageTaskReference[]) => void;
  removeQueueReference: (localUrl: string) => void;
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
  imageOutputDirectory: '',
  setImageOutputDirectory: (imageOutputDirectory) => set({ imageOutputDirectory }),
  queueReferences: [],
  addQueueReferences: (references) =>
    set((state) => ({
      queueReferences: [
        ...state.queueReferences,
        ...references.filter(
          (ref) => !state.queueReferences.some((known) => known.localUrl === ref.localUrl),
        ),
      ],
    })),
  removeQueueReference: (localUrl) =>
    set((state) => ({
      queueReferences: state.queueReferences.filter((ref) => ref.localUrl !== localUrl),
    })),
  previewImage: null,
  setPreviewImage: (previewImage) => set({ previewImage }),
}));
