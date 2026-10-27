import { useEffect } from 'react';
import { useAccountsLive, useWailsEvent } from '../../../hooks';
import { AppEvent } from '../../../types';
import type { MuseJob } from '../../../types';
import { useMuseStore } from './museStore';

/** Loads Muse state and the accounts on mount and keeps them current from backend events. */
export function useMuseLive() {
  const load = useMuseStore((s) => s.load);
  const loadJobs = useMuseStore((s) => s.loadJobs);
  const setJob = useMuseStore((s) => s.setJob);
  const loadOptions = useMuseStore((s) => s.loadOptions);
  useAccountsLive();

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<MuseJob | null>(AppEvent.MuseJobUpdated, setJob);
  // Bulk changes (clear finished, remove selected) carry no payload: reload
  // the jobs (the options did not change).
  useWailsEvent(AppEvent.MuseJobsChanged, () => void loadJobs());
  // An account check reads Muse's limits (attachments, models): reload the
  // options so the screens offer them without leaving the section.
  useWailsEvent(AppEvent.AccountsUpdated, () => void loadOptions());
}
