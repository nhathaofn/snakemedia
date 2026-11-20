import React, { useState } from 'react';
import { Button, Card } from '../../../components/ui';
import { api, unwrap } from '../../../services';
import type { Account, AccountProvider } from '../../../types';
import { accountProviderLabels } from '../../../utils';

/** The form shown above the account list. */
export type PanelMode =
  | { kind: 'browser' }
  | { kind: 'cookies' }
  | { kind: 'replace'; account: Account }
  | { kind: 'rename'; account: Account };

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
      return `Thêm tài khoản ${site} (Chrome)`;
    case 'cookies':
      return `Thêm tài khoản ${site} từ cookie`;
    case 'replace':
      return `Dán cookie mới cho "${mode.account.name}"`;
    case 'rename':
      return `Đổi tên "${mode.account.name}"`;
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
            Tạo hồ sơ Chrome riêng cho tài khoản {site}. Chrome chưa mở ngay để bạn chỉnh proxy (mặc
            định không dùng proxy), sau đó tự bấm &quot;Mở Chrome&quot; trên thẻ để đăng nhập.
          </p>
        )}

        <div className="accounts-form-actions">
          <Button type="submit" variant="primary" isLoading={busy}>
            {mode.kind === 'browser' ? 'Tạo tài khoản' : 'Lưu'}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Huỷ
          </Button>
        </div>
      </form>
    </Card>
  );
};
