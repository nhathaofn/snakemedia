import { create } from 'zustand';
import { api, unwrap } from '../../../services';
import type { MuseAccount, MuseJob, MuseOptions } from '../../../types';
import { upsertJob } from './format';

interface MuseState {
  jobs: MuseJob[];
  accounts: MuseAccount[];
  options: MuseOptions | null;
  loaded: boolean;
  loadError: string | null;
  /** Loads jobs, accounts and options once; later calls reload them. */
  load: () => Promise<void>;
  setJob: (job: MuseJob | null) => void;
  dropJob: (id: string) => void;
  setAccounts: (accounts: MuseAccount[] | null) => void;
  setAccount: (account: MuseAccount) => void;
}

/** Shared Muse state, kept current by backend events (see useMuseLive). */
export const useMuseStore = create<MuseState>((set) => ({
  jobs: [],
  accounts: [],
  options: null,
  loaded: false,
  loadError: null,
  load: async () => {
    // A failed call must surface as loadError: showing "no jobs" or "no ready
    // account" when the backend failed would mislead the user.
    try {
      const [jobs, accounts, options] = await Promise.all([
        api.listMuseJobs().then(unwrap),
        api.getMuseAccounts().then(unwrap),
        api.getMuseOptions().then(unwrap),
      ]);
      set({ jobs, accounts, options, loaded: true, loadError: null });
    } catch (cause) {
      set({ loaded: true, loadError: cause instanceof Error ? cause.message : String(cause) });
    }
  },
  setJob: (job) => {
    // Events can carry no job (bulk changes use MuseJobsChanged); ignore them.
    if (!job?.id) return;
    set((state) => ({ jobs: upsertJob(state.jobs, job) }));
  },
  dropJob: (id) => set((state) => ({ jobs: state.jobs.filter((job) => job.id !== id) })),
  setAccounts: (accounts) => {
    if (Array.isArray(accounts)) set({ accounts });
  },
  setAccount: (account) =>
    set((state) => ({
      accounts: state.accounts.some((item) => item.id === account.id)
        ? state.accounts.map((item) => (item.id === account.id ? account : item))
        : [...state.accounts, account],
    })),
}));
