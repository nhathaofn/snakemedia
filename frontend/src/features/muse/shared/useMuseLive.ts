import { useEffect } from 'react';
import { useWailsEvent } from '../../../hooks';
import { AppEvent } from '../../../types';
import type { MuseAccount, MuseJob } from '../../../types';
import { useMuseStore } from './museStore';

/** Loads Muse state on mount and keeps it current from backend events. */
export function useMuseLive() {
  const load = useMuseStore((s) => s.load);
  const setJob = useMuseStore((s) => s.setJob);
  const setAccounts = useMuseStore((s) => s.setAccounts);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<MuseJob | null>(AppEvent.MuseJobUpdated, setJob);
  // Bulk changes (clear finished, remove selected) carry no payload: reload.
  useWailsEvent(AppEvent.MuseJobsChanged, () => void load());
  useWailsEvent<MuseAccount[] | null>(AppEvent.MuseAccountsUpdated, setAccounts);
}
