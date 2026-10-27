import { describe, expect, it, vi } from 'vitest';
import { AppConfig, ResponseCode } from '../types';
import type { ApiResponse } from '../types';
import { saveConfigPatch } from './useConfigUpdate';

const config = (revision: number, pollIntervalMs: number, headless: boolean) =>
  AppConfig.createFrom({ revision, accounts: { pollIntervalMs, headless } });

describe('saveConfigPatch', () => {
  it('reapplies a section change on top of the newest config after a conflict', async () => {
    const newest = config(3, 2000, false);
    const send = vi
      .fn<(cfg: AppConfig) => Promise<ApiResponse<AppConfig>>>()
      .mockResolvedValueOnce({ success: false, code: ResponseCode.Conflict, data: newest })
      .mockImplementationOnce(async (cfg) => ({ success: true, code: ResponseCode.OK, data: cfg }));

    const res = await saveConfigPatch(
      config(2, 1500, true),
      (cfg) => ({ accounts: { ...cfg.accounts, headless: true } }),
      send,
    );

    expect(send).toHaveBeenCalledTimes(2);
    expect(res.data?.revision).toBe(3);
    // Another screen's pollIntervalMs survives; this screen's headless lands.
    expect(res.data?.accounts.pollIntervalMs).toBe(2000);
    expect(res.data?.accounts.headless).toBe(true);
  });

  it('does not retry other failures', async () => {
    const send = vi.fn(async () => ({ success: false, code: ResponseCode.InvalidArgument }));
    const res = await saveConfigPatch(config(1, 1500, true), { theme: undefined }, send);
    expect(send).toHaveBeenCalledTimes(1);
    expect(res.success).toBe(false);
  });
});
