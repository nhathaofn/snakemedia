import { create } from 'zustand';
import type { FlowImageTaskReference, FlowVideoModel, GeneratedVideoItem } from '../../../../types';
import { createReferenceSlice, createStudioSlice } from '../../shared';
import type { ReferenceSlice, StudioSlice } from '../../shared';

export interface FlowVideoState
  extends StudioSlice<FlowVideoModel, GeneratedVideoItem>, ReferenceSlice {
  duration: number;
  setDuration: (seconds: number) => void;
  resolution: string;
  setResolution: (resolution: string) => void;
  allAspectRatio: string;
  setAllAspectRatio: (ratio: string) => void;
  allDuration: number;
  setAllDuration: (seconds: number) => void;
  allResolution: string;
  setAllResolution: (resolution: string) => void;
  videoOutputDirectory: string;
  setVideoOutputDirectory: (directory: string) => void;
  /** References (in the media store) given to prompts added to the All queue next. */
  queueReferences: FlowImageTaskReference[];
  addQueueReferences: (references: FlowImageTaskReference[]) => void;
  removeQueueReference: (localUrl: string) => void;
  previewVideo: GeneratedVideoItem | null;
  setPreviewVideo: (video: GeneratedVideoItem | null) => void;
}

/** Keeps a choice when the model offers it, otherwise takes the model's first. */
const keep = <T>(current: T, offered: readonly T[] | undefined, fallback: T): T =>
  offered?.includes(current) ? current : (offered?.[0] ?? fallback);

export const useFlowVideoStore = create<FlowVideoState>()((set) => ({
  ...createStudioSlice<FlowVideoModel, GeneratedVideoItem, FlowVideoState>(set, (model, state) => ({
    aspectRatio: keep(state.aspectRatio, model?.aspectRatios, ''),
    // Longest duration first, as Flow preselects it.
    duration: keep(state.duration, model?.durations.slice().reverse(), 0),
    resolution: keep(state.resolution, model?.resolutions, ''),
    allAspectRatio: keep(state.allAspectRatio, model?.aspectRatios, ''),
    allDuration: keep(state.allDuration, model?.durations.slice().reverse(), 0),
    allResolution: keep(state.allResolution, model?.resolutions, ''),
  })),
  ...createReferenceSlice<FlowVideoState>(set),
  duration: 0,
  setDuration: (duration) => set({ duration }),
  resolution: '',
  setResolution: (resolution) => set({ resolution }),
  allAspectRatio: '',
  setAllAspectRatio: (allAspectRatio) => set({ allAspectRatio }),
  allDuration: 0,
  setAllDuration: (allDuration) => set({ allDuration }),
  allResolution: '',
  setAllResolution: (allResolution) => set({ allResolution }),
  videoOutputDirectory: '',
  setVideoOutputDirectory: (videoOutputDirectory) => set({ videoOutputDirectory }),
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
  previewVideo: null,
  setPreviewVideo: (previewVideo) => set({ previewVideo }),
}));
