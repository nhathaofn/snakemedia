import { create } from 'zustand';

export type AspectRatio = '16:9' | '4:3' | '1:1' | '3:4' | '9:16';

export interface ReferenceImage {
  base64: string;
  name: string;
  mimeType: string;
  previewUrl: string;
}

export interface GeneratedImageItem {
  id: string;
  url: string;
  prompt: string;
  model: string;
  aspectRatio: AspectRatio;
  createdAt: string;
}

export interface FlowImageState {
  selectedModel: string;
  setSelectedModel: (model: string) => void;
  aspectRatio: AspectRatio;
  setAspectRatio: (ratio: AspectRatio) => void;
  quantity: number;
  setQuantity: (qty: number) => void;
  referenceImage: ReferenceImage | null;
  setReferenceImage: (image: ReferenceImage | null) => void;
  prompt: string;
  setPrompt: (prompt: string) => void;
  isGenerating: boolean;
  setIsGenerating: (generating: boolean) => void;
  images: GeneratedImageItem[];
  addImages: (newImages: GeneratedImageItem[]) => void;
  previewImage: GeneratedImageItem | null;
  setPreviewImage: (image: GeneratedImageItem | null) => void;
}

export const useFlowImageStore = create<FlowImageState>((set) => ({
  selectedModel: '',
  setSelectedModel: (selectedModel) => set({ selectedModel }),
  aspectRatio: '1:1',
  setAspectRatio: (aspectRatio) => set({ aspectRatio }),
  quantity: 1,
  setQuantity: (quantity) => set({ quantity }),
  referenceImage: null,
  setReferenceImage: (referenceImage) => set({ referenceImage }),
  prompt: '',
  setPrompt: (prompt) => set({ prompt }),
  isGenerating: false,
  setIsGenerating: (isGenerating) => set({ isGenerating }),
  images: [],
  addImages: (newImages) => set((state) => ({ images: [...newImages, ...state.images] })),
  previewImage: null,
  setPreviewImage: (previewImage) => set({ previewImage }),
}));
