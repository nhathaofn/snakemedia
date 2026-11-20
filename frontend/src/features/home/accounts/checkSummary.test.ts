import { describe, expect, it } from 'vitest';
import { AccountProvider, AccountSource, AccountStatus } from '../../../types';
import type { Account } from '../../../types';
import { summarizeAccountCheck, summarizeBatchCheck, videoCheckTargets } from './checkSummary';

const now = new Date('2026-11-19T12:00:00Z').getTime();

const makeAccount = (patch: Partial<Account> = {}): Account =>
  ({
    id: 'a1',
    provider: AccountProvider.Muse,
    name: 'Nick 1',
    source: AccountSource.Browser,
    status: AccountStatus.SignedIn,
    enabled: true,
    usage: [],
    proxy: '',
    createdAt: '',
    updatedAt: '',
    ...patch,
  }) as Account;

describe('checkSummary', () => {
  it('reports dead/video-restricted, signed-out, paused, and ready accounts individually', () => {
    const fatal = summarizeAccountCheck(
      'Nick 1',
      {
        success: false,
        error: 'Tài khoản Muse đã chết hoặc bị khóa',
        data: makeAccount({ status: AccountStatus.Error, enabled: false }),
      },
      now,
    );
    expect(fatal.type).toBe('error');
    expect(fatal.message).toContain('Nick 1');
    expect(fatal.message).toContain('bị khóa');

    const imageOnly = summarizeAccountCheck(
      'Nick 1',
      {
        success: true,
        data: makeAccount({
          status: AccountStatus.SignedIn,
          enabled: true,
          skipVideoCheck: true,
          message: 'Không tạo được video (đã tắt kiểm tra video, chỉ tạo ảnh)',
        }),
      },
      now,
    );
    expect(imageOnly.type).toBe('info');
    expect(imageOnly.message).toContain('chỉ tạo ảnh');

    const signedOut = summarizeAccountCheck(
      'Nick 1',
      {
        success: true,
        data: makeAccount({ status: AccountStatus.SignedOut, message: 'Hết phiên.' }),
      },
      now,
    );
    expect(signedOut.type).toBe('error');
    expect(signedOut.message).toContain('Hết phiên.');

    const limit24h = summarizeAccountCheck(
      'Nick 1',
      {
        success: true,
        data: makeAccount({
          enabled: false,
          autoResume: true,
          pausedUntil: '2026-11-20T12:00:00Z',
        }),
      },
      now,
    );
    expect(limit24h.type).toBe('info');
    expect(limit24h.message).toContain('24g');

    const ok = summarizeAccountCheck('Nick 1', { success: true, data: makeAccount() }, now);
    expect(ok.type).toBe('success');
  });

  it('breaks down checkAll totals across all account outcomes', () => {
    const summary = summarizeBatchCheck(
      [
        { name: 'A', res: { success: true, data: makeAccount({ name: 'A' }) } },
        {
          name: 'A2',
          res: {
            success: true,
            data: makeAccount({ name: 'A2', skipVideoCheck: true }),
          },
        },
        {
          name: 'B',
          res: {
            success: false,
            error: 'banned',
            data: makeAccount({ name: 'B', status: AccountStatus.Error, enabled: false }),
          },
        },
        {
          name: 'C',
          res: {
            success: true,
            data: makeAccount({ name: 'C', status: AccountStatus.SignedOut }),
          },
        },
        {
          name: 'D',
          res: {
            success: true,
            data: makeAccount({
              name: 'D',
              enabled: false,
              autoResume: true,
              pausedUntil: '2026-11-20T12:00:00Z',
            }),
          },
        },
      ],
      now,
    );
    expect(summary.type).toBe('error');
    expect(summary.message).toBe(
      'Đã kiểm tra 5 tài khoản: 1 sẵn sàng, 1 chỉ tạo ảnh, 1 chết/lỗi đăng nhập, 1 chưa đăng nhập, 1 tạm nghỉ/chờ 24h.',
    );

    const videoSummary = summarizeBatchCheck(
      [{ name: 'A', res: { success: true, data: makeAccount({ name: 'A' }) } }],
      now,
      'kiểm tra tạo video',
    );
    expect(videoSummary.message).toBe('Đã kiểm tra tạo video 1 tài khoản: 1 sẵn sàng.');
  });

  it('selects only enabled, unpaused, video-eligible Muse accounts for video check', () => {
    const picked = videoCheckTargets(
      [
        makeAccount({ id: 'ok', status: AccountStatus.SignedIn }),
        makeAccount({ id: 'unverified', status: AccountStatus.Unverified }),
        makeAccount({ id: 'flow', provider: AccountProvider.Flow }),
        makeAccount({ id: 'skip', skipVideoCheck: true }),
        makeAccount({ id: 'off', enabled: false }),
        makeAccount({ id: 'paused', pausedUntil: '2026-11-20T12:00:00Z' }),
        makeAccount({ id: 'out', status: AccountStatus.SignedOut }),
        makeAccount({ id: 'signing', status: AccountStatus.SigningIn }),
        makeAccount({ id: 'err', status: AccountStatus.Error }),
      ],
      now,
    );
    expect(picked.map((a) => a.id)).toEqual(['ok', 'unverified']);
  });
});
