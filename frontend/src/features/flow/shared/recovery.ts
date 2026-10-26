import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../../../services/api';
import { useAppStore } from '../../../stores';
import type { ApiResponse } from '../../../types';
import { clearGenerationAttempt } from './hooks';
import type { ConversationTurn, RestoredTurn, StudioResult } from './studioStore';

/** Fields a saved job's results carry to rebuild its conversation turn. */
export interface RecoverableItem extends StudioResult {
  prompt: string;
  aspectRatio: string;
  pending?: boolean;
}

/** The parts of a studio store that job recovery reads and writes. */
interface RecoverableStore<TItem> {
  getState: () => {
    turns: ConversationTurn[];
    addItems: (items: TItem[]) => void;
    restoreTurns: (entries: RestoredTurn<TItem>[]) => void;
    completeTurn: (turnId: string, items: TItem[]) => void;
  };
}

/** One turn per job, keyed by the job (request) ID, in the order jobs were listed. */
export function turnsFromJobs<TItem extends RecoverableItem>(
  items: TItem[],
  pendingLabel: string,
): RestoredTurn<TItem>[] {
  const jobs = new Map<string, TItem[]>();
  for (const item of items) {
    if (!item.jobId) continue;
    jobs.set(item.jobId, [...(jobs.get(item.jobId) ?? []), item]);
  }
  return [...jobs].map(([jobId, results]) => ({
    turn: {
      id: jobId,
      prompt: results[0].prompt,
      attachments: [],
      pendingCount: 0,
      pendingRatio: results[0].aspectRatio,
      pendingLabel,
    },
    items: results,
  }));
}

/**
 * Restores a studio's saved jobs when it opens. Jobs already in the
 * conversation (sent this session, or restored on an earlier visit) are kept
 * as they are.
 */
export function useRestoreJobs<TItem extends RecoverableItem>(
  store: RecoverableStore<TItem>,
  list: () => Promise<ApiResponse<TItem[]>>,
  pendingLabel: string,
): void {
  useEffect(() => {
    let active = true;
    void list().then((response) => {
      if (!active || !response.success || !response.data?.length) return;
      store.getState().restoreTurns(turnsFromJobs(response.data, pendingLabel));
    });
    return () => {
      active = false;
    };
  }, [store, list, pendingLabel]);
}

/** Stops waiting on a running request; Flow keeps a job it already accepted. */
export function useStopGeneration(): (requestId: string) => void {
  const addToast = useAppStore((s) => s.addToast);
  return useCallback(
    (requestId: string) => {
      void api.cancelGeneration(requestId).then((response) => {
        if (!response.success || !response.data) {
          addToast({
            type: 'info',
            title: 'Yêu cầu đã kết thúc',
            message: response.error ?? 'Không còn gì đang chạy để dừng.',
          });
        }
      });
    },
    [addToast],
  );
}

/**
 * Checks a saved job again without sending a new request. Several jobs can be
 * checked at once; the same job is never checked twice in parallel.
 */
export function useResumeJob<TItem extends RecoverableItem>(
  store: RecoverableStore<TItem>,
  resume: (jobId: string) => Promise<ApiResponse<TItem[]>>,
): { resuming: ReadonlySet<string>; resumeJob: (jobId: string) => Promise<void> } {
  const addToast = useAppStore((s) => s.addToast);
  const running = useRef(new Set<string>());
  const [resuming, setResuming] = useState<ReadonlySet<string>>(new Set());

  const resumeJob = useCallback(
    async (jobId: string) => {
      if (running.current.has(jobId)) return;
      running.current.add(jobId);
      setResuming(new Set(running.current));
      try {
        const response = await resume(jobId);
        if (!response.success || !response.data) {
          addToast({
            type: 'error',
            title: 'Không kiểm tra được job',
            message: response.error ?? 'Không rõ lỗi.',
          });
          return;
        }
        const current = store.getState();
        current.addItems(response.data);
        // Show what finished so far, even while other results are still pending.
        if (current.turns.some((turn) => turn.id === jobId)) {
          current.completeTurn(jobId, response.data);
        }
        if (!response.data.some((item) => item.pending)) clearGenerationAttempt(jobId);
      } finally {
        running.current.delete(jobId);
        setResuming(new Set(running.current));
      }
    },
    [store, resume, addToast],
  );

  return { resuming, resumeJob };
}
