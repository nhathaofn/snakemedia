import { create } from 'zustand';
import type { FlowVideoModel, GeneratedVideoItem } from '../../../../types';
import { createReferenceSlice, createStudioSlice } from '../../shared';
import type { ReferenceSlice, StudioSlice } from '../../shared';

export interface FlowVideoState
  extends StudioSlice<FlowVideoModel, GeneratedVideoItem>, ReferenceSlice {
  duration: number;
  setDuration: (seconds: number) => void;
  resolution: string;
  setResolution: (resolution: string) => void;
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
  })),
  ...createReferenceSlice<FlowVideoState>(set),
  duration: 0,
  setDuration: (duration) => set({ duration }),
  resolution: '',
  setResolution: (resolution) => set({ resolution }),
}));
