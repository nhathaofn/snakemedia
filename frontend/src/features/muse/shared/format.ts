import { MuseAccountStatus, MuseJobStatus } from '../../../types';
import type { MuseAccount, MuseJob, MuseQuota } from '../../../types';

/** One job per non-empty line, as the backend expects in `prompts`. */
export function splitPrompts(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line !== '');
}

export const jobStatusLabels: Record<MuseJobStatus, string> = {
  [MuseJobStatus.Queued]: 'Chờ chạy',
  [MuseJobStatus.Processing]: 'Đang chạy',
  [MuseJobStatus.Succeeded]: 'Xong',
  [MuseJobStatus.Failed]: 'Lỗi',
  [MuseJobStatus.Cancelled]: 'Đã huỷ',
};

export const accountStatusLabels: Record<MuseAccountStatus, string> = {
  [MuseAccountStatus.Unverified]: 'Chưa kiểm tra',
  [MuseAccountStatus.SigningIn]: 'Đang chờ đăng nhập',
  [MuseAccountStatus.SignedIn]: 'Đã đăng nhập',
  [MuseAccountStatus.SignedOut]: 'Chưa đăng nhập',
  [MuseAccountStatus.Error]: 'Lỗi',
};

/** Replaces the job with the same ID, or puts a new job first (newest first). */
export function upsertJob(jobs: readonly MuseJob[], job: MuseJob): MuseJob[] {
  const index = jobs.findIndex((item) => item.id === job.id);
  if (index < 0) return [job, ...jobs];
  const next = jobs.slice();
  next[index] = job;
  return next;
}

/** True while the account rests after Muse said it hit a limit. */
export function accountPaused(account: MuseAccount, now: number): boolean {
  return !!account.pausedUntil && new Date(account.pausedUntil).getTime() > now;
}

/** True when the account can take new jobs at `now`, judged from what is stored. */
export function accountReady(account: MuseAccount, now: number): boolean {
  return (
    account.enabled && account.status === MuseAccountStatus.SignedIn && !accountPaused(account, now)
  );
}

/** Short quota summary, e.g. "Đã dùng 40% · đặt lại 28/10 09:00". */
export function quotaSummary(quota: MuseQuota | undefined): string {
  if (!quota) return 'Chưa đọc được hạn mức';
  const parts = [`Đã dùng ${Math.round(quota.percentUsed)}%`];
  if (quota.resetsAt) {
    parts.push(
      `đặt lại ${new Date(quota.resetsAt).toLocaleString('vi-VN', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' })}`,
    );
  }
  if (quota.topupTotal > 0) parts.push(`top-up ${quota.topupBalance}/${quota.topupTotal}`);
  return parts.join(' · ');
}

export const isVideoUrl = (url: string) => /\.(mp4|webm|mov|m4v)$/i.test(url);
