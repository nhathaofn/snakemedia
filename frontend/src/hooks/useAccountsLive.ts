import { useEffect } from 'react';
import { useAccountsStore } from '../stores';
import { AppEvent } from '../types';
import type { Account } from '../types';
import { useWailsEvent } from './useWailsEvent';

/** Loads the account list on mount and keeps it current from backend events. */
export function useAccountsLive() {
  const load = useAccountsStore((s) => s.load);
  const setAccounts = useAccountsStore((s) => s.setAccounts);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<Account[] | null>(AppEvent.AccountsUpdated, setAccounts);
}
