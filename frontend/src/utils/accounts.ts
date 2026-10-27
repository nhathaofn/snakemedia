import { AccountProvider, AccountStatus } from '../types';
import type { Account } from '../types';

/** Provider names as the account screens show them. */
export const accountProviderLabels: Record<AccountProvider, string> = {
  [AccountProvider.Flow]: 'Google Flow',
  [AccountProvider.Muse]: 'Muse',
};

export const accountStatusLabels: Record<AccountStatus, string> = {
  [AccountStatus.Unverified]: 'Chưa kiểm tra',
  [AccountStatus.SigningIn]: 'Đang chờ đăng nhập',
  [AccountStatus.SignedIn]: 'Đã đăng nhập',
  [AccountStatus.SignedOut]: 'Chưa đăng nhập',
  [AccountStatus.Error]: 'Lỗi',
};

/** True while the account rests after its site reported a limit. */
export function accountPaused(account: Account, now: number): boolean {
  return !!account.pausedUntil && new Date(account.pausedUntil).getTime() > now;
}

/** True when the account can take new work at `now`, judged from what is stored. */
export function accountReady(account: Account, now: number): boolean {
  return (
    account.enabled &&
    account.status === AccountStatus.SignedIn &&
    !account.noRoute &&
    !accountPaused(account, now)
  );
}

/** Local date and time for an RFC 3339 value, or '' when there is none. */
export const formatAccountTime = (value?: string) =>
  value ? new Date(value).toLocaleString('vi-VN') : '';
