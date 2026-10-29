import { describe, expect, it, vi } from 'vitest';
import { onNextFrame } from './frameBatch';

describe('onNextFrame', () => {
  it('applies everything scheduled in one frame, in order', () => {
    vi.useFakeTimers();
    const seen: number[] = [];
    onNextFrame(() => seen.push(1));
    onNextFrame(() => seen.push(2));
    expect(seen).toEqual([]);
    vi.advanceTimersByTime(16);
    expect(seen).toEqual([1, 2]);
    onNextFrame(() => seen.push(3));
    vi.advanceTimersByTime(16);
    expect(seen).toEqual([1, 2, 3]);
    vi.useRealTimers();
  });
});
