import { useEffect } from 'react';
import { onNextFrame, useAccountsLive, useWailsEvent } from '../../../hooks';
import { AppEvent } from '../../../types';
import type { MuseJob } from '../../../types';
import { useMuseStore } from './museStore';

/** Loads Muse state and the accounts on mount and keeps them current from backend events. */
export function useMuseLive() {
  const load = useMuseStore((s) => s.load);
  const dropJobs = useMuseStore((s) => s.dropJobs);
  const setJob = useMuseStore((s) => s.setJob);
  const loadOptions = useMuseStore((s) => s.loadOptions);
  useAccountsLive();

  useEffect(() => {
    void load();
  }, [load]);

  // Events are applied once per frame: a busy queue re-renders at most once a frame.
  useWailsEvent<MuseJob | null>(AppEvent.MuseJobUpdated, (job) => onNextFrame(() => setJob(job)));
  // Bulk removals (clear finished, remove selected, trim) carry the removed
  // IDs: drop them instead of reloading every job.
  useWailsEvent<string[] | null>(AppEvent.MuseJobsRemoved, (ids) =>
    onNextFrame(() => dropJobs(ids)),
  );
  // An account check reads Muse's limits (attachments, models): reload the
  // options so the screens offer them without leaving the section.
  useWailsEvent(AppEvent.AccountsUpdated, () => void loadOptions());
}
