import { useRef } from 'react';
import { useAppStore } from '../../../stores';
import type { ApiResponse } from '../../../types';
import { errorMessage } from '../../../utils';
import { UNCERTAIN_RETRY_HINT, beginGenerationAttempt, useGeneration } from './hooks';
import type { GenerationLifecycle, GenerationState } from './hooks';
import type { ConversationTurnDraft } from './studioStore';

/** The turn actions of a studio store (Image, Video, Edit). */
interface TurnState<TItem> {
  beginTurn: (turn: ConversationTurnDraft) => void;
  completeTurn: (turnId: string, items: TItem[]) => void;
  failTurn: (turnId: string, error: string) => void;
  removeTurn: (turnId: string) => void;
}

/**
 * The conversation side of one generation: the turn appears when it starts,
 * gets its results, or shows why it failed. A failure whose prompt went back
 * into the box removes the turn; an uncertain one says how to reconcile.
 * `settle` reports the request ID's fate (pending = keep it for a retry).
 */
export function turnLifecycle<TItem extends { pending?: boolean }>(
  getState: () => TurnState<TItem>,
  turn: ConversationTurnDraft,
  settle: (pending: boolean) => void = () => {},
): GenerationLifecycle<TItem> {
  const fail = (message: string, promptRestored: boolean) => {
    const current = getState();
    if (promptRestored) current.removeTurn(turn.id);
    else current.failTurn(turn.id, message);
  };
  return {
    onStart: () => getState().beginTurn(turn),
    onSuccess: (results) => {
      settle(results.some((result) => result.pending));
      getState().completeTurn(turn.id, results);
    },
    onFailure: (message, promptRestored) => {
      settle(false);
      fail(message, promptRestored);
    },
    onUncertainFailure: (message, promptRestored) => {
      settle(true);
      fail(`${message} ${UNCERTAIN_RETRY_HINT}`, promptRestored);
    },
  };
}

/** A studio store read at call time: generation state plus its turns. */
interface StudioStore<TItem> {
  getState: () => GenerationState<TItem> & TurnState<TItem>;
}

/** What one submit sends: the turn to show and the request to fingerprint. */
export interface StudioDraft<TRequest> {
  turn: Omit<ConversationTurnDraft, 'id'>;
  request: TRequest;
}

/**
 * Submit for the Image and Video studios: derives the request ID from the
 * request's content (beginGenerationAttempt), shows the turn under that ID so
 * restoring or resuming finds it, and sends. A second submit while the ID is
 * being derived is ignored, so one Enter never sends the same prompt twice.
 */
export function useStudioSubmit<TItem extends { error?: string; pending?: boolean }, TRequest>(
  store: StudioStore<TItem>,
  kind: 'image' | 'video',
  noun: string,
  send: (request: TRequest & { requestId: string }) => Promise<ApiResponse<TItem[]>>,
) {
  const generate = useGeneration<TItem>(store, noun);
  const addToast = useAppStore((s) => s.addToast);
  const submitting = useRef(false);

  return (draft: StudioDraft<TRequest>) => {
    if (submitting.current) return;
    submitting.current = true;
    void beginGenerationAttempt(kind, draft.request)
      .then((attempt) => {
        submitting.current = false;
        const turn = { ...draft.turn, id: attempt.requestId };
        return generate(
          () => send({ ...draft.request, requestId: attempt.requestId }),
          turnLifecycle(store.getState, turn, attempt.settle),
        );
      })
      .catch((error: unknown) => {
        submitting.current = false;
        addToast({ type: 'error', title: 'Không gửi được yêu cầu', message: errorMessage(error) });
      });
  };
}
