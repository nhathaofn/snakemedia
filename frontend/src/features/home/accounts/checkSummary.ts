import { AccountProvider, AccountStatus } from '../../../types';
import type { Account } from '../../../types';
import { accountPaused, formatAccountTime, formatRemainingTime } from '../../../utils';

export type AccountCheckResponse = {
  success: boolean;
  error?: string;
  data?: Account;
};

export type CheckToastReport = {
  type: 'success' | 'info' | 'error';
  message: string;
};

export type CheckOutcomeItem = {
  name: string;
  res: AccountCheckResponse;
};

export function summarizeAccountCheck(
  accountName: string,
  res: AccountCheckResponse,
  now = Date.now(),
): CheckToastReport {
  const acc = res.data;
  const label = acc?.name || accountName;
  if (!res.success || acc?.status === AccountStatus.Error) {
    return {
      type: 'error',
      message: `${label}: ${res.error || acc?.message || 'Tài khoản bị lỗi hoặc không tạo được video.'}`,
    };
  }
  if (acc?.status === AccountStatus.SignedOut) {
    return {
      type: 'error',
      message: `${label}: ${acc.message || 'Chưa đăng nhập hoặc phiên đã hết hạn.'}`,
    };
  }
  if (acc && accountPaused(acc, now)) {
    const untilText = formatAccountTime(acc.pausedUntil);
    const remain = formatRemainingTime(acc.pausedUntil, now);
    const windowText = remain ? `${untilText} (còn ${remain})` : untilText;
    return {
      type: 'info',
      message: `${label}: ${acc.message || `Đang tạm nghỉ, dùng lại được lúc ${windowText}.`}`,
    };
  }
  if (acc && !acc.enabled) {
    return {
      type: 'info',
      message: `${label}: Đã đăng nhập nhưng tài khoản đang tắt.`,
    };
  }
  if (acc?.skipVideoCheck) {
    return {
      type: 'info',
      message: `${label}: ${acc.message || 'Còn sống (đã tắt kiểm tra tạo video, chỉ tạo ảnh).'}`,
    };
  }
  return {
    type: 'success',
    message: `${label}: ${acc?.message || 'Hoạt động bình thường.'}`,
  };
}

export function summarizeBatchCheck(
  items: readonly CheckOutcomeItem[],
  now = Date.now(),
  actionLabel = 'kiểm tra',
): CheckToastReport {
  let ready = 0;
  let videoDisabled = 0;
  let paused = 0;
  let signedOut = 0;
  let error = 0;
  let disabled = 0;

  for (const item of items) {
    const acc = item.res.data;
    if (!item.res.success || acc?.status === AccountStatus.Error) {
      error++;
    } else if (acc?.status === AccountStatus.SignedOut) {
      signedOut++;
    } else if (acc && accountPaused(acc, now)) {
      paused++;
    } else if (acc && !acc.enabled) {
      disabled++;
    } else if (acc?.skipVideoCheck) {
      videoDisabled++;
    } else {
      ready++;
    }
  }

  const parts: string[] = [];
  if (ready > 0) parts.push(`${ready} sẵn sàng`);
  if (videoDisabled > 0) parts.push(`${videoDisabled} chỉ tạo ảnh`);
  if (error > 0) parts.push(`${error} chết/lỗi đăng nhập`);
  if (signedOut > 0) parts.push(`${signedOut} chưa đăng nhập`);
  if (paused > 0) parts.push(`${paused} tạm nghỉ/chờ 24h`);
  if (disabled > 0) parts.push(`${disabled} đang tắt`);

  const detail = parts.length > 0 ? `: ${parts.join(', ')}` : '';
  const type: CheckToastReport['type'] =
    error > 0 || signedOut > 0 ? 'error' : paused > 0 || videoDisabled > 0 ? 'info' : 'success';

  return {
    type,
    message: `Đã ${actionLabel} ${items.length} tài khoản${detail}.`,
  };
}

/** Returns enabled Muse accounts still eligible for an explicit video-creation probe. */
export function videoCheckTargets(accounts: readonly Account[], now = Date.now()): Account[] {
  return accounts.filter(
    (a) =>
      a.provider === AccountProvider.Muse &&
      !a.skipVideoCheck &&
      a.enabled &&
      !accountPaused(a, now) &&
      a.status !== AccountStatus.SignedOut &&
      a.status !== AccountStatus.Error &&
      a.status !== AccountStatus.SigningIn,
  );
}
