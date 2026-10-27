import React, { useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { Cookie, ListChecks, LogIn } from 'lucide-react';
import './AccountsPage.css';
import { Button, Icon, SegmentedControl } from '../../../components/ui';
import { ACCOUNT_CHECK_CONCURRENCY } from '../../../constants';
import { useAccountsLive, useNow } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAccountsStore, useAppStore } from '../../../stores';
import { AccountProvider } from '../../../types';
import type { Account } from '../../../types';
import { accountProviderLabels, accountReady } from '../../../utils';
import { AccountCard } from './AccountCard';
import type { AccountAction } from './AccountCard';
import { AccountPanel } from './AccountPanel';
import type { PanelMode } from './AccountPanel';
import { proxyLines } from './AccountPanel';
import { runLimited } from './runLimited';
import { ROUTES } from '../../../routes/registry';

const providers = Object.values(AccountProvider);

const providerHints: Record<AccountProvider, string> = {
  [AccountProvider.Flow]:
    'Tài khoản Google cho Flow. Ảnh và video luân phiên trên các tài khoản đang bật, đã đăng nhập và còn credits.',
  [AccountProvider.Muse]:
    'Tài khoản muse.ai. Hàng đợi Muse chia tác vụ cho các tài khoản đang bật theo Cài đặt Muse.',
};

/** Work running on an account (its action) or on the page's account panel. */
type BusyWork = AccountAction | 'panel';

const accountBusy = (work?: BusyWork): AccountAction | null =>
  work && work !== 'panel' ? work : null;

const isProvider = (value: string | null): value is AccountProvider =>
  providers.includes(value as AccountProvider);

/**
 * Every provider's accounts in one place: one Chrome profile each, signed in
 * through a window or imported cookies, each on its proxy from the shared list.
 * `?provider=muse` opens a provider's tab.
 */
export const AccountsPage: React.FC = () => {
  useAccountsLive();
  const now = useNow();
  const [params, setParams] = useSearchParams();
  const requested = params.get('provider');
  const provider = isProvider(requested) ? requested : AccountProvider.Flow;
  const accounts = useAccountsStore((s) => s.accounts);
  const loaded = useAccountsStore((s) => s.loaded);
  const loadError = useAccountsStore((s) => s.loadError);
  const setAccount = useAccountsStore((s) => s.setAccount);
  const addToast = useAppStore((s) => s.addToast);
  const [panel, setPanel] = useState<PanelMode | null>(null);
  // What is running, per account id (or 'panel' / 'all' for page work): one
  // slow account never locks the others. The ref answers "already running?"
  // from handlers without waiting for a render.
  const [busy, setBusy] = useState<ReadonlyMap<string, BusyWork>>(new Map());
  const busyRef = useRef(new Map<string, BusyWork>());
  const mark = (key: string, value: BusyWork | null) => {
    const next = new Map(busyRef.current);
    if (value === null) next.delete(key);
    else next.set(key, value);
    busyRef.current = next;
    setBusy(next);
  };

  const list = accounts.filter((a) => a.provider === provider);
  const navigate = useNavigate();
  // Accounts left on the automatic proxy cannot run without a list: the app
  // never falls back to the real IP. Re-read when the panel closes (a proxy
  // may have been set) — the list itself is edited on the Proxy page.
  const [noProxies, setNoProxies] = useState(false);
  useEffect(() => {
    let active = true;
    void api.getProxies().then((res) => {
      if (active) setNoProxies(res.success && proxyLines(res.data ?? '').length === 0);
    });
    return () => {
      active = false;
    };
  }, [panel]);

  const report = (cause: unknown) =>
    addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });

  const run = async (key: string, label: BusyWork, action: () => Promise<void>) => {
    if (busyRef.current.has(key)) return;
    mark(key, label);
    try {
      await action();
    } catch (cause) {
      report(cause);
    } finally {
      mark(key, null);
    }
  };

  /** Applies a response that carries the account even when it failed. */
  const keep = <T extends { data?: Account }>(res: T) => {
    if (res.data) setAccount(res.data);
    return res;
  };

  const selectProvider = (next: AccountProvider) => {
    setPanel(null);
    setParams({ provider: next }, { replace: true });
  };

  // Checks run a few at a time (each may start a browser); accounts already
  // busy with something else are left out.
  const checkAll = () =>
    run('all', 'check', async () => {
      const targets = list.filter((a) => !busyRef.current.has(a.id));
      const results = await runLimited(targets, ACCOUNT_CHECK_CONCURRENCY, async (account) => {
        mark(account.id, 'check');
        try {
          return keep(await api.checkAccount(account.id)).success;
        } finally {
          mark(account.id, null);
        }
      });
      const failed = results.filter((ok) => ok !== true).length;
      addToast({
        type: failed ? 'error' : 'success',
        message: `Đã kiểm tra ${targets.length} tài khoản${failed ? `, ${failed} lỗi` : ''}.`,
      });
    });

  const onAction = (action: AccountAction, account: Account) => {
    const key = account.id;
    switch (action) {
      case 'check':
        return run(key, action, async () => void unwrap(keep(await api.checkAccount(account.id))));
      case 'open':
        return run(key, action, async () =>
          setAccount(unwrap(await api.openAccountBrowser(account.id))),
        );
      case 'toggle':
        return run(key, action, async () =>
          setAccount(unwrap(await api.setAccountEnabled(account.id, !account.enabled))),
        );
      case 'rotate':
        return run(key, action, async () => {
          const res = unwrap(await api.rotateAccountIP(account.id));
          addToast({ type: 'success', message: `IP mới của ${account.name}: ${res.ip}` });
        });
      case 'proxy':
        return setPanel({ kind: 'proxy', account });
      case 'cookies':
        return setPanel({ kind: 'replace', account });
      case 'rename':
        return setPanel({ kind: 'rename', account });
      case 'remove':
        if (
          !window.confirm(
            `Xoá "${account.name}" và toàn bộ dữ liệu Chrome của tài khoản này (cookie, cache, đăng nhập)? Tài khoản trên ${accountProviderLabels[account.provider]} không bị xoá.`,
          )
        )
          return;
        return run(key, action, async () => {
          unwrap(await api.removeAccount(account.id));
          useAccountsStore.setState((s) => ({
            accounts: s.accounts.filter((a) => a.id !== account.id),
          }));
        });
    }
  };

  const submitPanel = (save: () => Promise<Account>) =>
    run('panel', 'panel', async () => {
      const account = await save();
      setAccount(account);
      if (panel?.kind === 'browser') {
        addToast({ type: 'info', message: 'Đã mở Chrome. Hãy đăng nhập trong cửa sổ đó.' });
      }
      setPanel(null);
    });

  const ready = list.filter((a) => accountReady(a, now)).length;
  return (
    <div className="page-shell accounts-page">
      <div className="accounts-header">
        <div className="accounts-heading">
          <h1 className="accounts-title">Tài khoản</h1>
          <SegmentedControl
            aria-label="Nhà cung cấp"
            options={providers.map((p) => ({
              value: p,
              label: `${accountProviderLabels[p]} (${accounts.filter((a) => a.provider === p).length})`,
            }))}
            value={provider}
            onChange={selectProvider}
          />
        </div>
        <div className="accounts-actions">
          <Button
            variant="primary"
            disabled={busy.has('panel')}
            onClick={() => setPanel({ kind: 'browser' })}
          >
            <Icon icon={LogIn} />
            <span>Đăng nhập bằng Chrome</span>
          </Button>
          <Button disabled={busy.has('panel')} onClick={() => setPanel({ kind: 'cookies' })}>
            <Icon icon={Cookie} />
            <span>Dán cookie</span>
          </Button>
          {list.length > 0 && (
            <Button isLoading={busy.has('all')} disabled={busy.has('all')} onClick={checkAll}>
              <Icon icon={ListChecks} />
              <span>Kiểm tra tất cả</span>
            </Button>
          )}
        </div>
      </div>

      <p className="accounts-hint">
        {providerHints[provider]} {list.length > 0 && `Sẵn sàng: ${ready}/${list.length}.`}
      </p>

      {noProxies && (
        <div className="accounts-proxy-hint">
          <span>
            Danh sách proxy đang trống. Tài khoản để proxy &quot;Tự động&quot; sẽ không chạy cho tới
            khi có proxy; app không bao giờ dùng IP thật thay thế. Thêm proxy, hoặc chọn &quot;Không
            dùng proxy&quot; cho từng tài khoản.
          </span>
          <Button size="sm" onClick={() => navigate(ROUTES.home.proxy)}>
            Mở danh sách proxy
          </Button>
        </div>
      )}

      {panel && (
        <AccountPanel
          key={panel.kind + ('account' in panel ? panel.account.id : '')}
          mode={panel}
          provider={provider}
          busy={busy.has('panel')}
          submit={submitPanel}
          onCancel={() => setPanel(null)}
        />
      )}

      {loadError && (
        <div role="alert" className="accounts-error">
          {loadError}
        </div>
      )}

      {!loaded ? (
        <div className="accounts-empty">Đang tải tài khoản…</div>
      ) : list.length === 0 ? (
        <div className="accounts-empty">
          Chưa có tài khoản {accountProviderLabels[provider]}. Đăng nhập bằng Chrome hoặc dán cookie
          để bắt đầu.
        </div>
      ) : (
        <div className="accounts-grid">
          {list.map((account) => (
            <AccountCard
              key={account.id}
              account={account}
              now={now}
              busy={accountBusy(busy.get(account.id))}
              onAction={onAction}
            />
          ))}
        </div>
      )}
    </div>
  );
};
