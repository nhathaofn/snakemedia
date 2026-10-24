/** Fields every Flow model shares, whatever the media type. */
export interface StudioModel {
  id: string;
  name: string;
  default: boolean;
}

export type ConversationAttachmentKind = 'image' | 'video';

/** A small, session-only reference captured when the user sends a prompt. */
export interface ConversationAttachment {
  id: string;
  kind: ConversationAttachmentKind;
  name: string;
  url: string;
}

export interface ConversationTurnDraft {
  id: string;
  prompt: string;
  attachments: ConversationAttachment[];
  pendingCount: number;
  pendingRatio: string;
  pendingLabel: string;
}

export interface ConversationTurn extends ConversationTurnDraft {
  resultKeys: string[];
  error?: string;
}

export interface StudioResult {
  id: string;
  jobId?: string;
}

/** A video job keeps the same conversation key while its result is refreshed. */
export const conversationItemKey = (item: StudioResult): string => item.jobId || item.id;

/** State every Flow studio shares; each studio store adds its own fields. */
export interface StudioSlice<TModel extends StudioModel, TItem extends StudioResult> {
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
  /** True while one or more requests in this studio are active. */
  isGenerating: boolean;
  /** Number of requests currently running; keeps the busy state correct in parallel. */
  activeGenerations: number;
  beginGeneration: () => void;
  finishGeneration: () => void;
  /** Generated results, newest first. */
  items: TItem[];
  addItems: (items: TItem[]) => void;
  /** Prompts and result keys in the order they were submitted this session. */
  turns: ConversationTurn[];
  beginTurn: (turn: ConversationTurnDraft) => void;
  completeTurn: (turnId: string, items: TItem[]) => void;
  failTurn: (turnId: string, error: string) => void;
  removeTurn: (turnId: string) => void;
}

type Setter<S> = (partial: Partial<S> | ((state: S) => Partial<S>)) => void;

/**
 * Builds the shared studio state for a zustand store. `fitToModel` keeps the
 * studio's own settings valid whenever the selected model changes (e.g. a video
 * duration the new model does not offer).
 */
export function createStudioSlice<
  TModel extends StudioModel,
  TItem extends StudioResult,
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
    activeGenerations: 0,
    beginGeneration: () =>
      set(
        (state) =>
          ({
            activeGenerations: state.activeGenerations + 1,
            isGenerating: true,
          }) as Partial<S>,
      ),
    finishGeneration: () =>
      set((state) => {
        const activeGenerations = Math.max(0, state.activeGenerations - 1);
        return { activeGenerations, isGenerating: activeGenerations > 0 } as Partial<S>;
      }),
    items: [],
    addItems: (items) => set((state) => ({ items: [...items, ...state.items] }) as Partial<S>),
    turns: [],
    beginTurn: (turn) =>
      set((state) =>
        state.turns.some((saved) => saved.id === turn.id)
          ? {}
          : ({ turns: [...state.turns, { ...turn, resultKeys: [] }] } as Partial<S>),
      ),
    completeTurn: (turnId, items) =>
      set(
        (state) =>
          ({
            turns: state.turns.map((turn) =>
              turn.id === turnId
                ? { ...turn, resultKeys: items.map(conversationItemKey), pendingCount: 0 }
                : turn,
            ),
          }) as Partial<S>,
      ),
    failTurn: (turnId, error) =>
      set(
        (state) =>
          ({
            turns: state.turns.map((turn) =>
              turn.id === turnId ? { ...turn, pendingCount: 0, error } : turn,
            ),
          }) as Partial<S>,
      ),
    removeTurn: (turnId) =>
      set((state) => {
        const removed = state.turns.find((turn) => turn.id === turnId);
        const turns = state.turns.filter((turn) => turn.id !== turnId);
        const references =
          (state as S & { referenceImages?: { previewUrl: string }[] }).referenceImages ?? [];
        for (const attachment of removed?.attachments ?? []) {
          if (
            attachment.kind === 'image' &&
            attachment.url.startsWith('blob:') &&
            !references.some((image) => image.previewUrl === attachment.url) &&
            !turns.some((turn) => turn.attachments.some((saved) => saved.url === attachment.url))
          ) {
            URL.revokeObjectURL(attachment.url);
          }
        }
        return { turns } as Partial<S>;
      }),
  };
}

/** The selected model of a studio, if the catalogue has loaded. */
export function selectedModelOf<TModel extends StudioModel>(state: {
  models: TModel[];
  selectedModel: string;
}): TModel | undefined {
  return state.models.find((m) => m.id === state.selectedModel);
}
