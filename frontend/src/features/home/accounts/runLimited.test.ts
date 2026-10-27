import { describe, expect, it } from 'vitest';
import { runLimited } from './runLimited';

describe('runLimited', () => {
  it('never runs more than the limit at once and keeps item order', async () => {
    let running = 0;
    let peak = 0;
    const results = await runLimited([1, 2, 3, 4, 5, 6, 7], 3, async (n) => {
      running++;
      peak = Math.max(peak, running);
      await new Promise((resolve) => setTimeout(resolve, 5 * (8 - n)));
      running--;
      return n * 10;
    });
    expect(peak).toBe(3);
    expect(results).toEqual([10, 20, 30, 40, 50, 60, 70]);
  });

  it('keeps going after a failure', async () => {
    const results = await runLimited([1, 2, 3], 2, async (n) => {
      if (n === 2) throw new Error('down');
      return n;
    });
    expect(results).toEqual([1, undefined, 3]);
  });

  it('handles an empty list and a bad limit', async () => {
    expect(await runLimited([], 3, async (n: number) => n)).toEqual([]);
    expect(await runLimited([1, 2], 0, async (n) => n)).toEqual([1, 2]);
  });
});
