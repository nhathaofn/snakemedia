import { describe, expect, it } from 'vitest';
import { MuseAccountSource, MuseAccountStatus, MuseJobKind, MuseJobStatus } from '../../../types';
import type { MuseAccount, MuseJob } from '../../../types';
import { accountReady, quotaSummary, splitPrompts, upsertJob } from './format';

const job = (id: string, status: MuseJobStatus = MuseJobStatus.Queued) =>
  ({
    id,
    kind: MuseJobKind.Video,
    prompt: id,
    ratio: '9:16',
    count: 1,
    projectId: '',
    references: [],
    status,
    attempts: 0,
    files: [],
    createdAt: '',
  }) as MuseJob;

const account = (patch: Partial<MuseAccount> = {}) =>
  ({
    id: 'a',
    name: 'A',
    source: MuseAccountSource.Browser,
    profileDir: '',
    status: MuseAccountStatus.SignedIn,
    enabled: true,
    createdAt: '',
    updatedAt: '',
    ...patch,
  }) as MuseAccount;

describe('muse format helpers', () => {
  it('splits one prompt per non-empty line', () => {
    expect(splitPrompts('  a \r\n\n b\n   \nc')).toEqual(['a', 'b', 'c']);
    expect(splitPrompts('   ')).toEqual([]);
  });

  it('upserts jobs newest first', () => {
    const list = [job('1'), job('2')];
    expect(upsertJob(list, job('3')).map((j) => j.id)).toEqual(['3', '1', '2']);
    const updated = upsertJob(list, job('2', MuseJobStatus.Succeeded));
    expect(updated[1].status).toBe(MuseJobStatus.Succeeded);
    expect(list[1].status).toBe(MuseJobStatus.Queued);
  });

  it('judges account readiness from status, switch and pause', () => {
    const now = Date.parse('2026-10-26T12:00:00Z');
    expect(accountReady(account(), now)).toBe(true);
    expect(accountReady(account({ enabled: false }), now)).toBe(false);
    expect(accountReady(account({ status: MuseAccountStatus.SignedOut }), now)).toBe(false);
    expect(accountReady(account({ pausedUntil: '2026-10-26T13:00:00Z' }), now)).toBe(false);
    expect(accountReady(account({ pausedUntil: '2026-10-26T11:00:00Z' }), now)).toBe(true);
  });

  it('summarises quota', () => {
    expect(quotaSummary(undefined)).toBe('Chưa đọc được hạn mức');
    expect(
      quotaSummary({
        percentUsed: 40.4,
        status: '',
        tier: '',
        paid: false,
        topupBalance: 2,
        topupTotal: 5,
        label: '',
        subtitle: '',
        checkedAt: '',
      }),
    ).toBe('Đã dùng 40% · top-up 2/5');
  });
});
