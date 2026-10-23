import type { AccountProfile, LoginProcessInfo, LoginStatusInfo } from '../../../types';

export interface LoginMonitorOptions {
  info: LoginProcessInfo;
  check: () => Promise<LoginStatusInfo>;
  onStatus: (status: LoginStatusInfo) => void;
  onSuccess: (profile: AccountProfile) => void;
  onError: (message: string) => void;
  onChecking: (checking: boolean) => void;
}

/** One request at a time; the next delay starts after completion. Closing the
 * monitor prevents late responses from publishing state or scheduling work. */
export function createLoginMonitor(options: LoginMonitorOptions) {
  const { info } = options;
  let stopped = false;
  let pending = false;
  let delay = info.policy.pollIntervalMs;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const deadline = Date.parse(info.expiresAt);
  let expiry: ReturnType<typeof setTimeout> | undefined;

  const stop = () => {
    stopped = true;
    clearTimeout(timer);
    clearTimeout(expiry);
  };

  const check = async () => {
    if (stopped || pending) return;
    if (Date.now() >= deadline) {
      stop();
      options.onError('Đã hết thời gian chờ đăng nhập. Đóng cửa sổ này rồi thử lại.');
      return;
    }
    clearTimeout(timer);
    pending = true;
    options.onChecking(true);
    try {
      const status = await options.check();
      if (stopped) return;
      options.onStatus(status);
      if (status.isLoggedIn && status.profile) {
        stop();
        options.onSuccess(status.profile);
        return;
      }
      delay = info.policy.pollIntervalMs;
    } catch (error) {
      if (stopped) return;
      options.onError(error instanceof Error ? error.message : String(error));
      delay = Math.min(delay * 2, info.policy.pollMaxIntervalMs);
    } finally {
      pending = false;
      if (!stopped) options.onChecking(false);
    }
    if (!stopped) timer = setTimeout(() => void check(), Math.min(delay, deadline - Date.now()));
  };

  return {
    start: () => {
      if (!Number.isFinite(deadline) || deadline <= Date.now()) {
        stop();
        options.onError('Phiên đăng nhập đã hết hạn.');
        return;
      }
      expiry = setTimeout(() => {
        stop();
        options.onChecking(false);
        options.onError('Đã hết thời gian chờ đăng nhập. Đóng cửa sổ này rồi thử lại.');
      }, deadline - Date.now());
      void check();
    },
    check,
    stop,
  };
}
