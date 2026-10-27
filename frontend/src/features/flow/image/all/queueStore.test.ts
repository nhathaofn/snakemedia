import { describe, expect, it } from 'vitest';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { countTasks, matchesTaskFilter, mergeTasks, upsertTask } from './queueStore';

const task = (id: string, status: FlowImageTaskStatus, extra: Partial<FlowImageTask> = {}) =>
  ({ id, status, prompt: id, ...extra }) as FlowImageTask;

describe('image queue store helpers', () => {
  it('upserts in place and appends new tasks', () => {
    const tasks = [task('a', FlowImageTaskStatus.Queued), task('b', FlowImageTaskStatus.Queued)];
    const updated = upsertTask(tasks, task('a', FlowImageTaskStatus.Running));
    expect(updated.map((t) => [t.id, t.status])).toEqual([
      ['a', FlowImageTaskStatus.Running],
      ['b', FlowImageTaskStatus.Queued],
    ]);
    expect(upsertTask(tasks, task('c', FlowImageTaskStatus.Queued)).map((t) => t.id)).toEqual([
      'a',
      'b',
      'c',
    ]);
  });

  it('keeps events newer than an in-flight snapshot', () => {
    const snapshot = [
      task('a', FlowImageTaskStatus.Running),
      task('b', FlowImageTaskStatus.Queued),
    ];
    const touched = new Map<string, FlowImageTask | null>([
      ['a', task('a', FlowImageTaskStatus.Succeeded)],
      ['b', null],
    ]);
    expect(mergeTasks(snapshot, touched)).toEqual([task('a', FlowImageTaskStatus.Succeeded)]);
  });

  it('counts held tasks apart from those waiting to run', () => {
    expect(
      countTasks([
        task('a', FlowImageTaskStatus.Queued),
        task('b', FlowImageTaskStatus.Queued, { held: true }),
        task('c', FlowImageTaskStatus.Running),
        task('d', FlowImageTaskStatus.Succeeded),
        task('e', FlowImageTaskStatus.Failed),
        task('f', FlowImageTaskStatus.Cancelled),
      ]),
    ).toEqual({ running: 1, queued: 1, held: 1, succeeded: 1, failed: 2 });
  });

  it('filters held tasks apart from the ones waiting to run', () => {
    const held = task('h', FlowImageTaskStatus.Queued, { held: true });
    const waiting = task('w', FlowImageTaskStatus.Queued);
    expect(matchesTaskFilter(held, 'held')).toBe(true);
    expect(matchesTaskFilter(held, FlowImageTaskStatus.Queued)).toBe(false);
    expect(matchesTaskFilter(waiting, FlowImageTaskStatus.Queued)).toBe(true);
    expect(matchesTaskFilter(waiting, 'held')).toBe(false);
    expect(matchesTaskFilter(task('f', FlowImageTaskStatus.Failed), 'all')).toBe(true);
  });
});
