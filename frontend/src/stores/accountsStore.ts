import { create } from 'zustand';
import { api, unwrapList } from '../services';
import type { Account } from '../types';

interface AccountsState {
  /** Every provider's accounts in saved order; filter by `provider` where needed. */
  accounts: Account[];
  loaded: boolean;
  loadError: string | null;
  load: () => Promise<void>;
  setAccounts: (accounts: Account[] | null) => void;
  setAccount: (account: Account) => void;
}

const replaceAccount = (accounts: readonly Account[], account: Account): Account[] =>
  accounts.some((item) => item.id === account.id)
    ? accounts.map((item) => (item.id === account.id ? account : item))
    : [...accounts, account];

// Bumped by every full list the backend pushes (accounts:updated): a list call
// that started before it holds an older snapshot and is dropped.
let listVersion = 0;
// Single accounts set while a list call is in flight, applied over its result.
const touched = new Map<string, Account>();
let listsInFlight = 0;

/** Shared account list, kept current by backend events (see useAccountsLive). */
export const useAccountsStore = create<AccountsState>((set) => ({
  accounts: [],
  loaded: false,
  loadError: null,
  load: async () => {
    const version = listVersion;
    listsInFlight++;
    // A failed call must surface as loadError: "no account" when the backend
    // failed would mislead the user.
    try {
      const snapshot = unwrapList(await api.getAccounts());
      if (version !== listVersion) return;
      let accounts = snapshot;
      touched.forEach((account) => (accounts = replaceAccount(accounts, account)));
      set({ accounts, loaded: true, loadError: null });
    } catch (cause) {
      set({ loaded: true, loadError: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      if (--listsInFlight === 0) touched.clear();
    }
  },
  setAccounts: (accounts) => {
    if (!Array.isArray(accounts)) return;
    listVersion++;
    touched.clear();
    set({ accounts, loaded: true, loadError: null });
  },
  setAccount: (account) => {
    if (listsInFlight > 0) touched.set(account.id, account);
    set((state) => ({ accounts: replaceAccount(state.accounts, account) }));
  },
}));
