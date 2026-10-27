import React, { useEffect, useState } from 'react';
import { Button, Card } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import type { Account, AccountProvider } from '../../../types';
import { accountProviderLabels } from '../../../utils';

/** The form shown above the account list. */
export type PanelMode =
  | { kind: 'browser' }
  | { kind: 'cookies' }
  | { kind: 'replace'; account: Account }
  | { kind: 'rename'; account: Account }
  | { kind: 'proxy'; account: Account };

interface AccountPanelProps {
  mode: PanelMode;
  provider: AccountProvider;
  busy: boolean;
  /** Runs the save; the page shows errors and keeps the panel open on failure. */
  submit: (save: () => Promise<Account>) => void;
  onCancel: () => void;
}

const titles = (mode: PanelMode, provider: AccountProvider): string => {
  const site = accountProviderLabels[provider];
  switch (mode.kind) {
    case 'browser':
      return `Thêm tài khoản ${site}: đăng nhập trong Chrome`;
    case 'cookies':
      return `Thêm tài khoản ${site} từ cookie`;
    case 'replace':
      return `Dán cookie mới cho "${mode.account.name}"`;
    case 'rename':
      return `Đổi tên "${mode.account.name}"`;
    case 'proxy':
      return `Proxy cho "${mode.account.name}"`;
  }
};

/** One proxy line per non-comment line of the shared list. */
export const proxyLines = (text: string) =>
  text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#') && !line.startsWith('//'));

export const AccountPanel: React.FC<AccountPanelProps> = ({
  mode,
  provider,
  busy,
  submit,
  onCancel,
}) => {
  const [name, setName] = useState(mode.kind === 'rename' ? mode.account.name : '');
  const [cookies, setCookies] = useState('');
  const [proxyChoice, setProxyChoice] = useState('');
  const [customProxy, setCustomProxy] = useState('');
  const [listed, setListed] = useState<string[]>([]);

  useEffect(() => {
    if (mode.kind !== 'proxy') return;
    const current = mode.account.proxy;
    let active = true;
    void api.getProxies().then((res) => {
      if (!active) return;
      const lines = res.success && res.data ? proxyLines(res.data) : [];
      setListed(lines);
      if (current === '' || current === 'none' || lines.includes(current)) {
        setProxyChoice(current);
      } else {
        setProxyChoice('custom');
        setCustomProxy(current);
      }
    });
    return () => {
      active = false;
    };
  }, [mode]);

  const onSubmit = (event: React.FormEvent) => {
    event.preventDefault();
    switch (mode.kind) {
      case 'browser':
        return submit(async () => unwrap(await api.addAccount(provider, name)));
      case 'cookies':
        return submit(async () => unwrap(await api.importAccountCookies(provider, name, cookies)));
      case 'replace':
        return submit(async () =>
          unwrap(await api.replaceAccountCookies(mode.account.id, cookies)),
        );
      case 'rename':
        return submit(async () => unwrap(await api.renameAccount(mode.account.id, name)));
      case 'proxy': {
        const line = proxyChoice === 'custom' ? customProxy.trim() : proxyChoice;
        return submit(async () => unwrap(await api.setAccountProxy(mode.account.id, line)));
      }
    }
  };

  const site = accountProviderLabels[provider];
  return (
    <Card className="accounts-panel">
      <form className="accounts-form" onSubmit={onSubmit}>
        <div className="accounts-form-title">{titles(mode, provider)}</div>

        {(mode.kind === 'browser' || mode.kind === 'cookies' || mode.kind === 'rename') && (
          <label className="accounts-field">
            <span>Tên gợi nhớ{mode.kind !== 'rename' && ' (để trống: tự đặt)'}</span>
            <input
              className="accounts-input"
              value={name}
              maxLength={60}
              autoFocus
              placeholder={`Ví dụ: ${site} 01`}
              onChange={(e) => setName(e.target.value)}
            />
          </label>
        )}

        {(mode.kind === 'cookies' || mode.kind === 'replace') && (
          <label className="accounts-field">
            <span>
              Cookie của {site} (Cookie-Editor → Export → JSON, cookies.txt, hoặc chuỗi header)
            </span>
            <textarea
              className="accounts-input accounts-textarea"
              value={cookies}
              rows={6}
              spellCheck={false}
              autoFocus={mode.kind === 'replace'}
              onChange={(e) => setCookies(e.target.value)}
            />
          </label>
        )}

        {mode.kind === 'browser' && (
          <p className="accounts-hint">
            Một cửa sổ Chrome riêng cho tài khoản này sẽ mở {site}. Đăng nhập xong, app tự nhận và
            lưu phiên; tài khoản dùng proxy theo danh sách ở mục Proxy.
          </p>
        )}

        {mode.kind === 'proxy' && (
          <>
            <label className="accounts-field">
              <span>Chế độ</span>
              <select
                className="accounts-input"
                value={proxyChoice}
                onChange={(e) => setProxyChoice(e.target.value)}
              >
                <option value="">Tự chọn từ danh sách proxy</option>
                <option value="none">Không dùng proxy (kết nối trực tiếp)</option>
                {listed.map((line) => (
                  <option key={line} value={line}>
                    {line}
                  </option>
                ))}
                <option value="custom">Proxy riêng…</option>
              </select>
            </label>
            {proxyChoice === 'custom' && (
              <label className="accounts-field">
                <span>Dòng proxy (ip:port, ip:port:user:pass, socks5://…, proxyvn:KEY…)</span>
                <input
                  className="accounts-input"
                  value={customProxy}
                  autoFocus
                  onChange={(e) => setCustomProxy(e.target.value)}
                />
              </label>
            )}
            <p className="accounts-hint">
              Chrome của tài khoản khởi động lại với proxy mới ở lần dùng kế tiếp.
            </p>
          </>
        )}

        <div className="accounts-form-actions">
          <Button type="submit" variant="primary" isLoading={busy}>
            {mode.kind === 'browser' ? 'Mở Chrome' : 'Lưu'}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Huỷ
          </Button>
        </div>
      </form>
    </Card>
  );
};
