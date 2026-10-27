import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ResponseCode } from '../types';
import type * as Services from '../services';
import type { Account } from '../types';

const backend = vi.hoisted(() => ({ getAccounts: vi.fn() }));

vi.mock('../services', async (importOriginal) => {
  const actual = await importOriginal<typeof Services>();
  return { ...actual, api: { ...actual.api, ...backend } };
});

const { useAccountsStore } = await import('./accountsStore');

const ok = <T>(data: T) => ({ success: true, data, code: ResponseCode.OK });
const account = (id: string, name = id) => ({ id, name }) as Account;

describe('accountsStore', () => {
  beforeEach(() => {
    useAccountsStore.setState({ accounts: [], loaded: false, loadError: null });
    backend.getAccounts.mockReset();
  });

  it('drops a list call overtaken by a pushed list', async () => {
    let answer: (value: unknown) => void = () => {};
    backend.getAccounts.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const loading = useAccountsStore.getState().load();
    useAccountsStore.getState().setAccounts([account('a', 'new'), account('b')]);
    answer(ok([account('a', 'old')]));
    await loading;
    expect(useAccountsStore.getState().accounts.map((a) => a.name)).toEqual(['new', 'b']);
  });

  it('keeps single updates made while the list loads', async () => {
    let answer: (value: unknown) => void = () => {};
    backend.getAccounts.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const loading = useAccountsStore.getState().load();
    useAccountsStore.getState().setAccount(account('a', 'checked'));
    answer(ok([account('a', 'old'), account('b')]));
    await loading;
    expect(useAccountsStore.getState().accounts.map((a) => a.name)).toEqual(['checked', 'b']);
  });

  it('treats a null list as no accounts', async () => {
    backend.getAccounts.mockResolvedValue(ok(null));
    await useAccountsStore.getState().load();
    expect(useAccountsStore.getState().accounts).toEqual([]);
    expect(useAccountsStore.getState().loadError).toBeNull();
  });
});
