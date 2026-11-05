import { describe, expect, it } from 'vitest';
import { FlowVideoTaskStatus } from '../../../../types';
import type { FlowVideoTask } from '../../../../types';
import { countTasks, matchesTaskFilter, mergeTasks, upsertTask } from './queueStore';

const task = (id: string, status: FlowVideoTaskStatus, extra: Partial<FlowVideoTask> = {}) =>
  ({ id, status, prompt: id, ...extra }) as FlowVideoTask;

describe('video queue store helpers', () => {
  it('upserts in place and appends new tasks', () => {
    const tasks = [task('a', FlowVideoTaskStatus.Queued), task('b', FlowVideoTaskStatus.Queued)];
    const updated = upsertTask(tasks, task('a', FlowVideoTaskStatus.Running));
    expect(updated.map((t) => [t.id, t.status])).toEqual([
      ['a', FlowVideoTaskStatus.Running],
      ['b', FlowVideoTaskStatus.Queued],
    ]);
    expect(upsertTask(tasks, task('c', FlowVideoTaskStatus.Queued)).map((t) => t.id)).toEqual([
      'a',
      'b',
      'c',
    ]);
  });

  it('keeps events newer than an in-flight snapshot', () => {
    const snapshot = [
      task('a', FlowVideoTaskStatus.Running),
      task('b', FlowVideoTaskStatus.Queued),
    ];
    const touched = new Map<string, FlowVideoTask | null>([
      ['a', task('a', FlowVideoTaskStatus.Succeeded)],
      ['b', null],
    ]);
    expect(mergeTasks(snapshot, touched)).toEqual([task('a', FlowVideoTaskStatus.Succeeded)]);
  });

  it('counts held tasks apart from those waiting to run', () => {
    expect(
      countTasks([
        task('a', FlowVideoTaskStatus.Queued),
        task('b', FlowVideoTaskStatus.Queued, { held: true }),
        task('c', FlowVideoTaskStatus.Running),
        task('d', FlowVideoTaskStatus.Succeeded),
        task('e', FlowVideoTaskStatus.Failed),
        task('f', FlowVideoTaskStatus.Cancelled),
      ]),
    ).toEqual({ running: 1, queued: 1, held: 1, succeeded: 1, failed: 2 });
  });

  it('filters held tasks apart from the ones waiting to run', () => {
    const held = task('h', FlowVideoTaskStatus.Queued, { held: true });
    const waiting = task('w', FlowVideoTaskStatus.Queued);
    expect(matchesTaskFilter(held, 'held')).toBe(true);
    expect(matchesTaskFilter(held, FlowVideoTaskStatus.Queued)).toBe(false);
    expect(matchesTaskFilter(waiting, FlowVideoTaskStatus.Queued)).toBe(true);
    expect(matchesTaskFilter(waiting, 'held')).toBe(false);
    expect(matchesTaskFilter(task('f', FlowVideoTaskStatus.Failed), 'all')).toBe(true);
  });
});
