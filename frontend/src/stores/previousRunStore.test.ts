import { describe, expect, it } from 'vitest';
import { latestOfPreviousRun } from './previousRunStore';

interface Item {
  id: string;
  at: string;
  done: boolean;
  screen: string;
  task: string;
}

const of = {
  id: (i: Item) => i.id,
  time: (i: Item) => i.at,
  finished: (i: Item) => i.done,
  screen: (i: Item) => i.screen,
  task: (i: Item) => i.task,
};
const previousStart = '2026-10-27T09:00:00.000Z';
const start = '2026-10-28T10:00:00.000Z';
const item = (id: string, at: string, screen: string, task: string, done = true): Item => ({
  id,
  at,
  done,
  screen,
  task,
});

describe('latestOfPreviousRun', () => {
  it('offers each screen only its last task of the run before this one', () => {
    const items = [
      item('a1', '2026-10-27T10:00:00.000Z', 'batch', 'prompts-1'),
      item('a2', '2026-10-27T10:05:00.000Z', 'batch', 'prompts-1'),
      item('b1', '2026-10-27T11:00:00.000Z', 'batch', 'prompts-2'),
      item('b2', '2026-10-27T11:01:00.000Z', 'batch', 'prompts-2'),
      item('s1', '2026-10-27T10:00:00.000Z', 'single', 's1'),
      item('s2', '2026-10-27T12:00:00.000Z', 'single', 's2'),
      item('old', '2026-10-26T12:00:00.000Z', 'single', 'old'),
      item('now', '2026-10-28T11:00:00.000Z', 'single', 'now'),
      item('running', '2026-10-27T13:00:00.000Z', 'single', 'running', false),
    ];
    expect([...latestOfPreviousRun(items, of, start, previousStart)].sort()).toEqual([
      'b1',
      'b2',
      's2',
    ]);
  });

  it('picks the task made last, not the one that finished last', () => {
    const items = [
      {
        ...item('first', '2026-10-27T10:10:00.000Z', 'single', 'first'),
        made: '2026-10-27T10:00:00.000Z',
      },
      {
        ...item('last', '2026-10-27T10:05:00.000Z', 'single', 'last'),
        made: '2026-10-27T10:01:00.000Z',
      },
    ];
    const made = { ...of, made: (i: Item & { made?: string }) => i.made };
    expect([...latestOfPreviousRun(items, made, start, previousStart)]).toEqual(['last']);
  });

  it('offers nothing when the run before this one made nothing', () => {
    const items = [item('old', '2026-10-26T12:00:00.000Z', 'single', 'old')];
    expect(latestOfPreviousRun(items, of, start, previousStart).size).toBe(0);
  });
});
