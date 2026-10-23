import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AccountProfile, LoginProcessInfo, LoginStatusInfo } from '../../../types';
import { createLoginMonitor } from './loginMonitor';

afterEach(() => vi.useRealTimers());

function setup(check: () => Promise<LoginStatusInfo>, timeout = 10000) {
  const info = {
    expiresAt: new Date(Date.now() + timeout).toISOString(),
    policy: { pollIntervalMs: 1000, pollMaxIntervalMs: 4000 },
  } as LoginProcessInfo;
  const callbacks = {
    onStatus: vi.fn(),
    onSuccess: vi.fn(),
    onError: vi.fn(),
    onChecking: vi.fn(),
  };
  const monitor = createLoginMonitor({ info, check, ...callbacks });
  return { monitor, ...callbacks };
}

describe('login polling lifecycle', () => {
  it('waits for the previous request and gates manual checks', async () => {
    vi.useFakeTimers();
    let resolve!: (value: LoginStatusInfo) => void;
    const check = vi.fn(
      () =>
        new Promise<LoginStatusInfo>((done) => {
          resolve = done;
        }),
    );
    const { monitor } = setup(check);
    monitor.start();
    await vi.advanceTimersByTimeAsync(3000);
    await monitor.check();
    expect(check).toHaveBeenCalledTimes(1);
    resolve({ isLoggedIn: false } as LoginStatusInfo);
    await vi.advanceTimersByTimeAsync(999);
    expect(check).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(check).toHaveBeenCalledTimes(2);
    monitor.stop();
  });

  it('publishes success once and stops polling', async () => {
    vi.useFakeTimers();
    const profile = { id: 'test-account' } as AccountProfile;
    const check = vi.fn(async () => ({ isLoggedIn: true, profile }) as LoginStatusInfo);
    const { monitor, onSuccess } = setup(check);
    monitor.start();
    await vi.advanceTimersByTimeAsync(9000);
    await monitor.check();
    expect(check).toHaveBeenCalledTimes(1);
    expect(onSuccess).toHaveBeenCalledExactlyOnceWith(profile);
  });

  it('backs off failed requests and respects the configured maximum', async () => {
    vi.useFakeTimers();
    const check = vi.fn(async () => {
      throw new Error('unavailable');
    });
    const { monitor } = setup(check, 20000);
    monitor.start();
    await vi.advanceTimersByTimeAsync(1999);
    expect(check).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(check).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(4000);
    expect(check).toHaveBeenCalledTimes(3);
    await vi.advanceTimersByTimeAsync(4000);
    expect(check).toHaveBeenCalledTimes(4);
    monitor.stop();
  });

  it('ignores late responses after closing or expiring', async () => {
    vi.useFakeTimers();
    for (const expired of [false, true]) {
      let resolve!: (value: LoginStatusInfo) => void;
      const check = vi.fn(
        () =>
          new Promise<LoginStatusInfo>((done) => {
            resolve = done;
          }),
      );
      const { monitor, onStatus, onSuccess, onError } = setup(check, 1000);
      monitor.start();
      if (expired) await vi.advanceTimersByTimeAsync(1000);
      else monitor.stop();
      resolve({ isLoggedIn: true, profile: { id: 'late' } } as LoginStatusInfo);
      await vi.advanceTimersByTimeAsync(10000);
      expect(onStatus).not.toHaveBeenCalled();
      expect(onSuccess).not.toHaveBeenCalled();
      expect(check).toHaveBeenCalledTimes(1);
      expect(onError).toHaveBeenCalledTimes(expired ? 1 : 0);
    }
  });
});
