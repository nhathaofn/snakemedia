import { describe, expect, it } from 'vitest';
import { MuseJobKind, MuseJobMode, MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';
import {
  fromPreviousRun,
  jobsOf,
  matchesFilter,
  mayBePaid,
  mergeSnapshot,
  selectedShown,
  upsertJob,
} from './format';

const job = (
  id: string,
  status: MuseJobStatus = MuseJobStatus.Queued,
  extra: Partial<Omit<MuseJob, 'convertValues'>> = {},
) =>
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
    ...extra,
  }) as unknown as MuseJob;

describe('muse format helpers', () => {
  it('upserts jobs newest first', () => {
    const list = [job('1'), job('2')];
    expect(upsertJob(list, job('3')).map((j) => j.id)).toEqual(['3', '1', '2']);
    const updated = upsertJob(list, job('2', MuseJobStatus.Succeeded));
    expect(updated[1].status).toBe(MuseJobStatus.Succeeded);
    expect(list[1].status).toBe(MuseJobStatus.Queued);
  });
});

describe('selectedShown', () => {
  it('keeps only selected jobs that are on screen, in screen order', () => {
    const shown = [{ id: 'b' }, { id: 'c' }] as MuseJob[];
    expect(selectedShown(shown, new Set(['a', 'c', 'b']))).toEqual(['b', 'c']);
    expect(selectedShown(shown, new Set(['a']))).toEqual([]);
  });
});

describe('mergeSnapshot', () => {
  const a = { id: 'a', prompt: 'old' } as MuseJob;
  const b = { id: 'b', prompt: 'b' } as MuseJob;

  it('keeps the snapshot when no event came in', () => {
    expect(mergeSnapshot([a, b], new Map())).toEqual([a, b]);
  });

  it('lets newer events win over the snapshot', () => {
    const newer = { ...a, prompt: 'new' } as MuseJob;
    const c = { id: 'c', prompt: 'c' } as MuseJob;
    const merged = mergeSnapshot(
      [a, b],
      new Map<string, MuseJob | null>([
        ['a', newer],
        ['b', null],
        ['c', c],
      ]),
    );
    expect(merged).toEqual([c, newer]);
  });
});

describe('jobsOf', () => {
  it('keeps one kind and mode, reading a missing mode as Hàng loạt', () => {
    const single = job('single', undefined, { mode: MuseJobMode.Single });
    const batch = job('batch', undefined, { mode: MuseJobMode.Batch });
    const old = job('old');
    const image = job('image', undefined, { kind: MuseJobKind.Image });
    const jobs = [single, batch, old, image];
    expect(jobsOf(jobs, MuseJobKind.Video, MuseJobMode.Single).map((j) => j.id)).toEqual([
      'single',
    ]);
    expect(jobsOf(jobs, MuseJobKind.Video, MuseJobMode.Batch).map((j) => j.id)).toEqual([
      'batch',
      'old',
    ]);
  });

  it('shows a storyboard image job with the video it opens', () => {
    const board = job('board', undefined, { kind: MuseJobKind.Image, boardFrames: 3 });
    expect(jobsOf([board], MuseJobKind.Video, MuseJobMode.Batch)).toEqual([board]);
    expect(jobsOf([board], MuseJobKind.Image, MuseJobMode.Batch)).toEqual([]);
  });
});

describe('matchesFilter', () => {
  it('matches held jobs whatever their status', () => {
    const held = job('h', undefined, { held: true });
    expect(matchesFilter(held, 'held')).toBe(true);
    expect(matchesFilter(job('q'), 'held')).toBe(false);
    expect(matchesFilter(job('q'), MuseJobStatus.Queued)).toBe(true);
    expect(matchesFilter(job('q'), 'all')).toBe(true);
  });
});

describe('mayBePaid', () => {
  it('flags jobs Muse took or may have taken', () => {
    expect(mayBePaid(job('a', MuseJobStatus.Failed))).toBe(false);
    expect(mayBePaid({ ...job('b', MuseJobStatus.Failed), sessionId: 's1' } as MuseJob)).toBe(true);
    expect(mayBePaid({ ...job('c', MuseJobStatus.Failed), sendUnknown: true } as MuseJob)).toBe(
      true,
    );
  });
});

describe('fromPreviousRun', () => {
  const start = '2026-10-28T10:00:00.000Z';
  it('hides only finished jobs made before this run', () => {
    const old = job('o', MuseJobStatus.Succeeded, { createdAt: '2026-10-27T10:00:00.000Z' });
    const oldRunning = job('r', MuseJobStatus.Processing, {
      createdAt: '2026-10-27T10:00:00.000Z',
    });
    const fresh = job('f', MuseJobStatus.Succeeded, { createdAt: '2026-10-28T11:00:00.000Z' });
    expect(fromPreviousRun(old, start)).toBe(true);
    expect(fromPreviousRun(oldRunning, start)).toBe(false);
    expect(fromPreviousRun(fresh, start)).toBe(false);
  });
});
