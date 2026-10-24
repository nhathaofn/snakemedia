import { create } from 'zustand';
import type { FlowModel, GeneratedImageItem } from '../../../../types';
import { createReferenceSlice, createStudioSlice } from '../../shared';
import type { ReferenceSlice, StudioSlice } from '../../shared';

export interface FlowImageState extends StudioSlice<FlowModel, GeneratedImageItem>, ReferenceSlice {
  previewImage: GeneratedImageItem | null;
  setPreviewImage: (image: GeneratedImageItem | null) => void;
}

export const useFlowImageStore = create<FlowImageState>()((set) => ({
  ...createStudioSlice<FlowModel, GeneratedImageItem, FlowImageState>(set),
  ...createReferenceSlice<FlowImageState>(set),
  previewImage: null,
  setPreviewImage: (previewImage) => set({ previewImage }),
}));
