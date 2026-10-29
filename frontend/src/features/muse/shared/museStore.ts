import { useMemo } from 'react';
import { create } from 'zustand';
import { api, unwrap, unwrapList } from '../../../services';
import { shownForRun, usePreviousRunStore } from '../../../stores';
import type { MuseJob, MuseOptions } from '../../../types';
import { jobRunAge, mergeSnapshot, offeredMuseJobs, upsertJob } from './format';

interface MuseState {
  jobs: MuseJob[];
  options: MuseOptions | null;
  loaded: boolean;
  loadError: string | null;
  /** Loads jobs and options; later calls reload them. */
  load: () => Promise<void>;
  /** Reloads the jobs only (bulk changes); calls made meanwhile share one reload. */
  loadJobs: () => Promise<void>;
  /**
   * Reloads the options only (an account check may have read Muse's limits);
   * calls made meanwhile share one reload, and a failure keeps the last options.
   */
  loadOptions: () => Promise<void>;
  setJob: (job: MuseJob | null) => void;
  dropJob: (id: string) => void;
  /** Drops jobs the backend removed at once (MuseJobsRemoved). */
  dropJobs: (ids: readonly string[] | null) => void;
}

const errorText = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause));

// Jobs events changed while a list call is in flight, applied over its
// snapshot when it lands (null: removed). Cleared once no call is in flight.
const touched = new Map<string, MuseJob | null>();
let listsInFlight = 0;
let jobsReload: Promise<void> | null = null;
let optionsReload: Promise<void> | null = null;

async function listJobs(): Promise<MuseJob[]> {
  listsInFlight++;
  try {
    const snapshot = unwrapList(await api.listMuseJobs());
    return mergeSnapshot(snapshot, touched);
  } finally {
    if (--listsInFlight === 0) touched.clear();
  }
}

/**
 * Shared Muse queue state, kept current by backend events (see useMuseLive).
 * Accounts live in the shared accounts store.
 */
export const useMuseStore = create<MuseState>((set, get) => ({
  jobs: [],
  options: null,
  loaded: false,
  loadError: null,
  load: async () => {
    // A failed call must surface as loadError: showing "no jobs" when the
    // backend failed would mislead the user.
    try {
      const [jobs, options] = await Promise.all([listJobs(), api.getMuseOptions().then(unwrap)]);
      set({ jobs, options, loaded: true, loadError: null });
    } catch (cause) {
      set({ loaded: true, loadError: errorText(cause) });
    }
  },
  loadJobs: () => {
    jobsReload ??= (async () => {
      try {
        set({ jobs: await listJobs(), loaded: true, loadError: null });
      } catch (cause) {
        set({ loaded: true, loadError: errorText(cause) });
      } finally {
        jobsReload = null;
      }
    })();
    return jobsReload;
  },
  loadOptions: () => {
    optionsReload ??= (async () => {
      try {
        set({ options: unwrap(await api.getMuseOptions()) });
      } catch {
        // The jobs are unaffected; the next account update tries again.
      } finally {
        optionsReload = null;
      }
    })();
    return optionsReload;
  },
  setJob: (job) => {
    // Events can carry no job (bulk removals use MuseJobsRemoved); ignore them.
    if (!job?.id) return;
    if (listsInFlight > 0) touched.set(job.id, job);
    set((state) => ({ jobs: upsertJob(state.jobs, job) }));
  },
  dropJob: (id) => {
    if (listsInFlight > 0) touched.set(id, null);
    set({ jobs: get().jobs.filter((job) => job.id !== id) });
  },
  dropJobs: (ids) => {
    if (!ids?.length) return;
    const gone = new Set(ids);
    if (listsInFlight > 0) gone.forEach((id) => touched.set(id, null));
    set({ jobs: get().jobs.filter((job) => !gone.has(job.id)) });
  },
}));

/**
 * The jobs the Muse screens list: of the run before this one, only each
 * screen's last task and only when the user chose to see it.
 */
export function useVisibleMuseJobs(): MuseJob[] {
  const jobs = useMuseStore((s) => s.jobs);
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
  const offered = useMemo(() => offeredMuseJobs(jobs), [jobs]);
  return useMemo(
    () => jobs.filter((job) => shownForRun(jobRunAge(job), job.id, offered, showPrevious)),
    [jobs, offered, showPrevious],
  );
}
