/** Fields every Flow model shares, whatever the media type. */
export interface StudioModel {
  id: string;
  name: string;
  default: boolean;
}

/** State every Flow studio shares; each studio store adds its own fields. */
export interface StudioSlice<TModel extends StudioModel, TItem> {
  /** Catalogue Flow reports for the account (GetImageModels / GetVideoModels). */
  models: TModel[];
  /** Replaces the catalogue and selects Flow's default model (or the first). */
  setModels: (models: TModel[]) => void;
  selectedModel: string;
  setSelectedModel: (id: string) => void;
  aspectRatio: string;
  setAspectRatio: (ratio: string) => void;
  quantity: number;
  setQuantity: (quantity: number) => void;
  prompt: string;
  setPrompt: (prompt: string) => void;
  isGenerating: boolean;
  setIsGenerating: (generating: boolean) => void;
  /** Generated results, newest first. */
  items: TItem[];
  addItems: (items: TItem[]) => void;
}

type Setter<S> = (partial: Partial<S> | ((state: S) => Partial<S>)) => void;

/**
 * Builds the shared studio state for a zustand store. `fitToModel` keeps the
 * studio's own settings valid whenever the selected model changes (e.g. a video
 * duration the new model does not offer).
 */
export function createStudioSlice<
  TModel extends StudioModel,
  TItem,
  S extends StudioSlice<TModel, TItem>,
>(
  set: Setter<S>,
  fitToModel: (model: TModel | undefined, state: S) => Partial<S> = () => ({}),
): StudioSlice<TModel, TItem> {
  const select = (state: S, id: string): Partial<S> => {
    const next = { ...state, selectedModel: id };
    const model = next.models.find((m) => m.id === id);
    return { selectedModel: id, ...fitToModel(model, next) } as Partial<S>;
  };

  return {
    models: [],
    setModels: (models) =>
      set((state) => {
        const preferred = models.find((m) => m.default) ?? models[0];
        return { models, ...select({ ...state, models }, preferred?.id ?? '') } as Partial<S>;
      }),
    selectedModel: '',
    setSelectedModel: (id) => set((state) => select(state, id)),
    aspectRatio: '',
    setAspectRatio: (aspectRatio) => set({ aspectRatio } as Partial<S>),
    quantity: 1,
    setQuantity: (quantity) => set({ quantity } as Partial<S>),
    prompt: '',
    setPrompt: (prompt) => set({ prompt } as Partial<S>),
    isGenerating: false,
    setIsGenerating: (isGenerating) => set({ isGenerating } as Partial<S>),
    items: [],
    addItems: (items) => set((state) => ({ items: [...items, ...state.items] }) as Partial<S>),
  };
}

/** The selected model of a studio, if the catalogue has loaded. */
export function selectedModelOf<TModel extends StudioModel>(state: {
  models: TModel[];
  selectedModel: string;
}): TModel | undefined {
  return state.models.find((m) => m.id === state.selectedModel);
}
