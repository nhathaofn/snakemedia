import React, { useState } from 'react';
import {
  Cookie,
  ExternalLink,
  LogIn,
  Power,
  RefreshCw,
  RotateCcw,
  Shield,
  Trash2,
  User,
} from 'lucide-react';
import '../shared/Muse.css';
import { Button, Card, Icon } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseAccountSource } from '../../../types';
import type { MuseAccount } from '../../../types';
import { accountPaused, accountStatusLabels, quotaSummary } from '../shared/format';
import { useMuseStore } from '../shared/museStore';
import { useMuseLive } from '../shared/useMuseLive';
import { useNow } from '../shared/useNow';

type FormMode =
  { kind: 'browser' } | { kind: 'cookies' } | { kind: 'replace'; account: MuseAccount };

const formatTime = (value?: string) => (value ? new Date(value).toLocaleString('vi-VN') : '');

export const MuseAccountsPage: React.FC = () => {
  useMuseLive();
  const now = useNow();
  const accounts = useMuseStore((s) => s.accounts);
  const loaded = useMuseStore((s) => s.loaded);
  const loadError = useMuseStore((s) => s.loadError);
  const setAccount = useMuseStore((s) => s.setAccount);
  const addToast = useAppStore((s) => s.addToast);
  const [form, setForm] = useState<FormMode | null>(null);
  const [name, setName] = useState('');
  const [cookies, setCookies] = useState('');
  const [busy, setBusy] = useState<string | null>(null);

  const [editingProxyAccount, setEditingProxyAccount] = useState<MuseAccount | null>(null);
  const [selectedProxy, setSelectedProxy] = useState('');
  const [customProxy, setCustomProxy] = useState('');
  const [availableProxies, setAvailableProxies] = useState<string[]>([]);

  const isRotatingProxy = (proxy?: string, proxyAuto?: string) => {
    const p = (proxy || proxyAuto || '').toLowerCase();
    return (
      p.startsWith('proxyvn:') ||
      p.startsWith('tmproxy:') ||
      p.startsWith('shoplike:') ||
      (p.startsWith('http://') && (p.includes('?') || p.includes('/api/'))) ||
      (p.startsWith('https://') && (p.includes('?') || p.includes('/api/')))
    );
  };

  const openProxyEdit = async (account: MuseAccount) => {
    setEditingProxyAccount(account);
    const p = account.proxy || '';
    if (p === 'none' || p === '') {
      setSelectedProxy(p);
      setCustomProxy('');
    } else {
      setSelectedProxy('custom');
      setCustomProxy(p);
    }
    try {
      const raw = unwrap(await api.getMuseProxies());
      const lines = raw
        .split('\n')
        .map((l) => l.trim())
        .filter((l) => l && !l.startsWith('#') && !l.startsWith('//'));
      setAvailableProxies(lines);
      if (lines.includes(p)) {
        setSelectedProxy(p);
      }
    } catch {
      setAvailableProxies([]);
    }
  };

  const saveProxy = (e: React.FormEvent) => {
    e.preventDefault();
    if (!editingProxyAccount) return;
    void run('proxy', async () => {
      const target = selectedProxy === 'custom' ? customProxy.trim() : selectedProxy;
      const updated = unwrap(await api.setMuseAccountProxy(editingProxyAccount.id, target));
      setAccount(updated);
      addToast({ type: 'success', message: `Đã cập nhật proxy cho "${updated.name}"` });
      setEditingProxyAccount(null);
    });
  };

  const rotateIP = (account: MuseAccount) => {
    void run(`rotate-${account.id}`, async () => {
      const res = unwrap(await api.rotateMuseAccountIP(account.id));
      addToast({
        type: 'success',
        message: `Đã đổi IP thành công cho ${account.name}: ${res.ip}`,
      });
      const accRes = await api.getMuseAccounts();
      if (accRes.data) useMuseStore.setState({ accounts: accRes.data });
    });
  };

  const run = async (key: string, action: () => Promise<void>) => {
    if (busy) return;
    setBusy(key);
    try {
      await action();
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    } finally {
      setBusy(null);
    }
  };

  const openForm = (mode: FormMode) => {
    setForm(mode);
    setName('');
    setCookies('');
  };

  const submitForm = (event: React.FormEvent) => {
    event.preventDefault();
    if (!form) return;
    void run('form', async () => {
      let account: MuseAccount;
      if (form.kind === 'browser') {
        account = unwrap(await api.addMuseAccount(name));
        addToast({ type: 'info', message: 'Đã mở Chrome. Hãy đăng nhập muse.ai trong cửa sổ đó.' });
      } else if (form.kind === 'cookies') {
        account = unwrap(await api.importMuseCookies(name, cookies));
      } else {
        account = unwrap(await api.replaceMuseCookies(form.account.id, cookies));
      }
      setAccount(account);
      setForm(null);
    });
  };

  const remove = (account: MuseAccount) => {
    if (!window.confirm(`Xoá tài khoản "${account.name}" và toàn bộ dữ liệu Chrome của nó?`))
      return;
    void run(account.id, async () => {
      unwrap(await api.removeMuseAccount(account.id));
      useMuseStore.setState((s) => ({ accounts: s.accounts.filter((a) => a.id !== account.id) }));
    });
  };

  return (
    <div className="page-shell muse-page">
      <div className="muse-header">
        <h1 className="muse-title">Tài khoản Muse</h1>
        <div className="muse-actions">
          <Button variant="primary" disabled={!!busy} onClick={() => openForm({ kind: 'browser' })}>
            <Icon icon={LogIn} />
            <span>Đăng nhập bằng Chrome</span>
          </Button>
          <Button disabled={!!busy} onClick={() => openForm({ kind: 'cookies' })}>
            <Icon icon={Cookie} />
            <span>Dán cookie</span>
          </Button>
        </div>
      </div>

      {form && (
        <Card className="muse-panel">
          <form className="muse-form" onSubmit={submitForm}>
            <div className="muse-form-title">
              {form.kind === 'browser' && 'Thêm tài khoản: đăng nhập trong Chrome'}
              {form.kind === 'cookies' && 'Thêm tài khoản từ cookie'}
              {form.kind === 'replace' && `Dán cookie mới cho "${form.account.name}"`}
            </div>
            {form.kind !== 'replace' && (
              <label className="muse-field">
                <span>Tên gợi nhớ</span>
                <input
                  className="muse-input"
                  value={name}
                  maxLength={60}
                  autoFocus
                  placeholder="Ví dụ: Muse 01"
                  onChange={(e) => setName(e.target.value)}
                />
              </label>
            )}
            {form.kind !== 'browser' && (
              <label className="muse-field">
                <span>
                  Cookie của muse.ai (Cookie-Editor → Export → JSON, hoặc cookies.txt, hoặc chuỗi
                  header)
                </span>
                <textarea
                  className="muse-input muse-textarea"
                  value={cookies}
                  rows={6}
                  spellCheck={false}
                  onChange={(e) => setCookies(e.target.value)}
                />
              </label>
            )}
            {form.kind === 'browser' && (
              <p className="muse-hint">
                Một cửa sổ Chrome riêng cho tài khoản này sẽ mở muse.ai. Đăng nhập xong, app tự nhận
                và lưu phiên.
              </p>
            )}
            <div className="muse-actions">
              <Button type="submit" variant="primary" isLoading={busy === 'form'}>
                {form.kind === 'browser' ? 'Mở Chrome' : 'Lưu'}
              </Button>
              <Button type="button" variant="ghost" onClick={() => setForm(null)}>
                Huỷ
              </Button>
            </div>
          </form>
        </Card>
      )}

      {editingProxyAccount && (
        <Card className="muse-panel">
          <form className="muse-form" onSubmit={saveProxy}>
            <div className="muse-form-title">
              <Icon icon={Shield} />
              <span>Cấu hình Proxy cho "{editingProxyAccount.name}"</span>
            </div>
            <label className="muse-field">
              <span>Chế độ Proxy</span>
              <select
                className="muse-input"
                value={selectedProxy}
                onChange={(e) => setSelectedProxy(e.target.value)}
              >
                <option value="">Mặc định: Tự chọn từ kho proxy</option>
                <option value="none">Không dùng proxy (Kết nối trực tiếp)</option>
                {availableProxies.map((p) => (
                  <option key={p} value={p}>
                    Kho: {p}
                  </option>
                ))}
                <option value="custom">Proxy riêng (nhập tuỳ chỉnh)...</option>
              </select>
            </label>
            {selectedProxy === 'custom' && (
              <label className="muse-field">
                <span>Nhập chuỗi proxy (IP:Port, SOCKS5, hoặc Key xoay)</span>
                <input
                  className="muse-input"
                  placeholder="ví dụ: 103.150.12.34:8080:user:pass hoặc proxyvn:KEY"
                  value={customProxy}
                  onChange={(e) => setCustomProxy(e.target.value)}
                  autoFocus
                />
              </label>
            )}
            <div className="muse-actions">
              <Button type="submit" variant="primary" isLoading={busy === 'proxy'}>
                Lưu Proxy
              </Button>
              <Button type="button" variant="ghost" onClick={() => setEditingProxyAccount(null)}>
                Huỷ
              </Button>
            </div>
          </form>
        </Card>
      )}

      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}

      {!loaded ? (
        <div className="muse-empty">Đang tải tài khoản…</div>
      ) : (accounts?.length ?? 0) === 0 ? (
        <div className="muse-empty">
          Chưa có tài khoản Muse. Đăng nhập bằng Chrome hoặc dán cookie để bắt đầu.
        </div>
      ) : (
        <div className="muse-grid">
          {(accounts ?? []).map((account) => (
            <Card key={account.id} className="muse-card">
              <div className="muse-card-head">
                <div className="muse-avatar">
                  <Icon icon={User} />
                </div>
                <div className="muse-card-meta">
                  <div className="muse-card-name" title={account.name}>
                    {account.name}
                  </div>
                  <div className={`muse-status muse-account-${account.status}`}>
                    <span className="muse-dot" />
                    <span>{accountStatusLabels[account.status]}</span>
                    {!account.enabled && <span className="muse-tag">Đang tắt</span>}
                    <span className="muse-tag">
                      {account.source === MuseAccountSource.Cookies ? 'Cookie' : 'Chrome'}
                    </span>
                  </div>
                </div>
              </div>
              <div className="muse-details">
                <div className="muse-row">
                  <span>Hạn mức</span>
                  <span>{quotaSummary(account.quota)}</span>
                </div>
                {account.quota?.tier && (
                  <div className="muse-row">
                    <span>Gói</span>
                    <span>{account.quota.tier}</span>
                  </div>
                )}
                {accountPaused(account, now) && (
                  <div className="muse-row">
                    <span>Tạm nghỉ tới</span>
                    <span>{formatTime(account.pausedUntil)}</span>
                  </div>
                )}
                <div className="muse-row">
                  <span>Kiểm tra gần nhất</span>
                  <span>{formatTime(account.lastCheckedAt) || 'Chưa kiểm tra'}</span>
                </div>
                <div className="muse-row">
                  <span>Proxy</span>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.375rem' }}>
                    <button
                      type="button"
                      className="muse-link-button"
                      title="Bấm để đổi proxy"
                      onClick={() => openProxyEdit(account)}
                    >
                      {account.proxy === 'none'
                        ? 'Trực tiếp (Không proxy)'
                        : account.proxy
                          ? account.proxy
                          : account.proxyAuto
                            ? `Tự chọn (${account.proxyAuto})`
                            : 'Tự chọn từ kho'}
                    </button>
                    {account.proxyStatus && (
                      <span
                        className={`muse-tag ${
                          account.proxyStatus === 'live' ? 'muse-tag-success' : 'muse-tag-danger'
                        }`}
                      >
                        {account.proxyStatus}
                      </span>
                    )}
                  </div>
                </div>
                {account.proxyIp && (
                  <div className="muse-row">
                    <span>IP ngoại mạng</span>
                    <span className="muse-mono">{account.proxyIp}</span>
                  </div>
                )}
              </div>
              {account.message && <div className="muse-note">{account.message}</div>}
              <div className="muse-actions muse-card-actions">
                {isRotatingProxy(account.proxy, account.proxyAuto) && (
                  <Button
                    size="sm"
                    isLoading={busy === `rotate-${account.id}`}
                    disabled={!!busy}
                    title="Yêu cầu IP mới từ nhà cung cấp proxy xoay"
                    onClick={() => rotateIP(account)}
                  >
                    <Icon icon={RotateCcw} size="sm" />
                    <span>Đổi IP</span>
                  </Button>
                )}
                <Button
                  size="sm"
                  disabled={!!busy}
                  title="Cấu hình proxy cho tài khoản"
                  onClick={() => openProxyEdit(account)}
                >
                  <Icon icon={Shield} size="sm" />
                  <span>Proxy</span>
                </Button>
                <Button
                  size="sm"
                  isLoading={busy === account.id}
                  disabled={!!busy}
                  onClick={() =>
                    run(account.id, async () => {
                      const res = await api.checkMuseAccount(account.id);
                      if (res.data) setAccount(res.data);
                      unwrap(res);
                    })
                  }
                >
                  <Icon icon={RefreshCw} size="sm" />
                  <span>Kiểm tra</span>
                </Button>
                <Button
                  size="sm"
                  disabled={!!busy}
                  onClick={() =>
                    run(account.id, async () => {
                      setAccount(unwrap(await api.openMuseAccountBrowser(account.id)));
                    })
                  }
                >
                  <Icon icon={ExternalLink} size="sm" />
                  <span>Mở Chrome</span>
                </Button>
                <Button
                  size="sm"
                  disabled={!!busy}
                  onClick={() => openForm({ kind: 'replace', account })}
                >
                  <Icon icon={Cookie} size="sm" />
                  <span>Cookie mới</span>
                </Button>
                <Button
                  size="sm"
                  disabled={!!busy}
                  onClick={() =>
                    run(account.id, async () => {
                      setAccount(
                        unwrap(await api.setMuseAccountEnabled(account.id, !account.enabled)),
                      );
                    })
                  }
                >
                  <Icon icon={Power} size="sm" />
                  <span>{account.enabled ? 'Tắt' : 'Bật'}</span>
                </Button>
                <Button
                  size="sm"
                  variant="danger"
                  disabled={!!busy}
                  onClick={() => remove(account)}
                >
                  <Icon icon={Trash2} size="sm" />
                  <span>Xoá</span>
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
};
