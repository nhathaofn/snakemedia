import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from '../../../services/api';
import {
  latestOfPreviousRun,
  runAge,
  shownForRun,
  useAppStore,
  usePreviousRunStore,
} from '../../../stores';
import type { RunAge } from '../../../stores';
import type { ApiResponse } from '../../../types';
import { clearGenerationAttempt } from './hooks';
import type { ConversationTurn, RestoredTurn, StudioResult } from './studioStore';

/** Fields a saved job's results carry to rebuild its conversation turn. */
export interface RecoverableItem extends StudioResult {
  prompt: string;
  aspectRatio: string;
  pending?: boolean;
  createdAt?: string;
}

/** The parts of a studio store that job recovery reads and writes. */
interface RecoverableStore<TItem> {
  getState: () => {
    turns: ConversationTurn[];
    addItems: (items: TItem[]) => void;
    restoreTurns: (entries: RestoredTurn<TItem>[]) => void;
    completeTurn: (turnId: string, items: TItem[]) => void;
    removeTurn?: (turnId: string) => void;
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

/**
 * Jobs of an earlier run the user checked again this run (useResumeJob):
 * their turns stay when the earlier run is hidden, since the user is working
 * with them now.
 */
const resumedThisRun = new Set<string>();

/** Which run of the app a listed job result was made in; pending ones are this run's. */
export function itemRunAge(item: RecoverableItem): RunAge {
  return runAge(item.createdAt, !item.pending);
}

/**
 * The jobs the start question offers again on a screen of single jobs: only
 * the last one the run before this one finished.
 */
export function offeredJobs(items: readonly RecoverableItem[]): Set<string> {
  return latestOfPreviousRun(
    items.filter((i) => i.jobId),
    {
      id: (i) => i.jobId ?? '',
      time: (i) => i.createdAt,
      finished: (i) => !i.pending,
      screen: () => '',
      task: (i) => i.jobId ?? '',
    },
  );
}

/**
 * Restores a studio's saved jobs like useRestoreJobs, but finished jobs from
 * the run before this one only while the user shows it (usePreviousRunStore)
 * and only the last of them (offeredJobs); other earlier jobs never. Jobs still pending always come back, so a paid job is
 * never lost from view. Switching the earlier run off takes its turns out of
 * the conversation again; nothing is deleted. Returns how many earlier jobs
 * there are, for the screen's "Phiên cũ" button.
 */
export function useRestoreJobsOfRun<TItem extends RecoverableItem>(
  store: RecoverableStore<TItem>,
  list: () => Promise<ApiResponse<TItem[]>>,
  pendingLabel: string,
): number {
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
  const [previousJobs, setPreviousJobs] = useState<string[]>([]);
  useEffect(() => {
    let active = true;
    void list().then((response) => {
      if (!active || !response.success || !response.data?.length) return;
      const items = response.data;
      const ageOf = (i: TItem): RunAge =>
        resumedThisRun.has(i.jobId ?? '') ? 'current' : itemRunAge(i);
      const offered = offeredJobs(items.filter((i) => ageOf(i) !== 'current'));
      const isShown = (i: TItem) => shownForRun(ageOf(i), i.jobId ?? '', offered, showPrevious);
      setPreviousJobs([...offered]);
      const shown = items.filter(isShown);
      if (shown.length) store.getState().restoreTurns(turnsFromJobs(shown, pendingLabel));
      const state = store.getState();
      for (const i of items) if (i.jobId && !isShown(i)) state.removeTurn?.(i.jobId);
    });
    return () => {
      active = false;
    };
  }, [store, list, pendingLabel, showPrevious]);
  return previousJobs.length;
}

/** The earlier run's finished video edits, as the start question words them. */
export function useReportPreviousEdits(): void {
  const report = usePreviousRunStore((s) => s.report);
  useEffect(() => {
    let active = true;
    report('flow-edit', null);
    void api
      .listVideoEditJobs()
      .then((res) => {
        const items = res.success ? (res.data ?? []) : [];
        const jobs = offeredJobs(items);
        if (active) report('flow-edit', jobs.size > 0 ? [`${jobs.size} video chỉnh sửa Flow`] : []);
      })
      .catch(() => active && report('flow-edit', []));
    return () => {
      active = false;
    };
  }, [report]);
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
      resumedThisRun.add(jobId);
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
