import React from 'react';
import {
  Cookie,
  ExternalLink,
  Pencil,
  Power,
  RefreshCw,
  RotateCcw,
  Shield,
  Trash2,
  User,
} from 'lucide-react';
import { Button, Card, Icon } from '../../../components/ui';
import { AccountSource } from '../../../types';
import type { Account } from '../../../types';
import { accountPaused, accountStatusLabels, formatAccountTime } from '../../../utils';

/** What a card asks its page to do. */
export type AccountAction =
  'check' | 'open' | 'proxy' | 'rotate' | 'cookies' | 'rename' | 'toggle' | 'remove';

interface AccountCardProps {
  account: Account;
  now: number;
  /** The action running on this account, or null when it is idle. */
  busy: AccountAction | null;
  onAction: (action: AccountAction, account: Account) => void;
}

/** How the account reaches the network, without secrets. */
export function proxyLabel(account: Account): string {
  if (account.proxy === 'none') return 'Không dùng proxy';
  if (account.proxy) return account.proxyInUse || 'Proxy riêng';
  return account.proxyInUse ? `Tự chọn · ${account.proxyInUse}` : 'Tự chọn từ danh sách';
}

export const AccountCard: React.FC<AccountCardProps> = ({ account, now, busy, onAction }) => {
  const working = (action: AccountAction) => busy === action;
  const act = (action: AccountAction) => () => onAction(action, account);
  const secondary: {
    action: AccountAction;
    icon: typeof Shield;
    label: string;
    hidden?: boolean;
  }[] = [
    { action: 'proxy', icon: Shield, label: 'Đổi proxy' },
    {
      action: 'rotate',
      icon: RotateCcw,
      label: 'Đổi IP proxy xoay',
      hidden: !account.proxyRotating,
    },
    { action: 'cookies', icon: Cookie, label: 'Dán cookie mới' },
    { action: 'rename', icon: Pencil, label: 'Đổi tên' },
    { action: 'toggle', icon: Power, label: account.enabled ? 'Tắt tài khoản' : 'Bật tài khoản' },
  ];

  return (
    <Card className="account-card">
      <div className="account-card-head">
        <div className="account-avatar">
          <Icon icon={User} />
        </div>
        <div className="account-card-meta">
          <div className="account-card-name" title={account.name}>
            {account.name}
          </div>
          {account.identity && account.identity !== account.name && (
            <div className="account-card-identity" title={account.identity}>
              {account.identity}
            </div>
          )}
          <div className={`account-status account-status-${account.status}`}>
            <span className="account-dot" />
            <span>{accountStatusLabels[account.status]}</span>
            <span className="account-tag">
              {account.source === AccountSource.Cookies ? 'Cookie' : 'Chrome'}
            </span>
            {!account.enabled && <span className="account-tag">Đang tắt</span>}
          </div>
        </div>
      </div>

      <div className="account-details">
        {account.usage.map((item) => (
          <div key={item.label} className="account-row">
            <span>{item.label}</span>
            <span>{item.value}</span>
          </div>
        ))}
        <div className="account-row">
          <span>Proxy</span>
          <span>
            <button type="button" className="account-link" onClick={act('proxy')}>
              {proxyLabel(account)}
            </button>
            {account.proxyIp && <span className="account-mono"> · {account.proxyIp}</span>}
          </span>
        </div>
        {accountPaused(account, now) && (
          <div className="account-row">
            <span>Tạm nghỉ tới</span>
            <span>{formatAccountTime(account.pausedUntil)}</span>
          </div>
        )}
        <div className="account-row">
          <span>Kiểm tra gần nhất</span>
          <span>{formatAccountTime(account.lastCheckedAt) || 'Chưa kiểm tra'}</span>
        </div>
      </div>

      {(account.message || account.noRoute) && (
        <div className="account-note">{account.noRoute || account.message}</div>
      )}

      <div className="account-actions">
        <Button size="sm" isLoading={working('check')} disabled={!!busy} onClick={act('check')}>
          <Icon icon={RefreshCw} size="sm" />
          <span>Kiểm tra</span>
        </Button>
        <Button size="sm" isLoading={working('open')} disabled={!!busy} onClick={act('open')}>
          <Icon icon={ExternalLink} size="sm" />
          <span>Mở Chrome</span>
        </Button>
        <div className="account-actions-icons">
          {secondary
            .filter((item) => !item.hidden)
            .map((item) => (
              <Button
                key={item.action}
                size="sm"
                variant="ghost"
                title={item.label}
                aria-label={item.label}
                isLoading={working(item.action)}
                disabled={!!busy}
                onClick={act(item.action)}
              >
                <Icon icon={item.icon} size="sm" />
              </Button>
            ))}
          <Button
            size="sm"
            variant="ghost"
            className="account-remove"
            title="Xoá tài khoản"
            aria-label="Xoá tài khoản"
            disabled={!!busy}
            onClick={act('remove')}
          >
            <Icon icon={Trash2} size="sm" />
          </Button>
        </div>
      </div>
    </Card>
  );
};
