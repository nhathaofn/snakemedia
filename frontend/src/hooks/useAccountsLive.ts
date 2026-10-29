import { useEffect } from 'react';
import { useAccountsStore } from '../stores';
import { AppEvent } from '../types';
import type { Account } from '../types';
import { useWailsEvent } from './useWailsEvent';

/**
 * Loads the account list on mount unless it is already loaded without error
 * (backend events keep a loaded list current), and keeps it current.
 */
export function useAccountsLive() {
  const load = useAccountsStore((s) => s.load);
  const setAccounts = useAccountsStore((s) => s.setAccounts);

  useEffect(() => {
    const { loaded, loadError } = useAccountsStore.getState();
    if (loaded && loadError === null) return;
    void load();
  }, [load]);

  useWailsEvent<Account[] | null>(AppEvent.AccountsUpdated, setAccounts);
}
