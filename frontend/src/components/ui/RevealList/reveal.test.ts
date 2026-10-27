import { describe, expect, it } from 'vitest';
import { nextRevealCount, revealCount } from './reveal';

describe('revealCount', () => {
  it('starts with one step, capped at the total', () => {
    expect(revealCount(0, 1000, 50)).toBe(50);
    expect(revealCount(0, 12, 50)).toBe(12);
    expect(revealCount(0, 0, 50)).toBe(0);
  });

  it('keeps what is shown and shrinks with the list', () => {
    expect(revealCount(150, 1000, 50)).toBe(150);
    expect(revealCount(150, 80, 50)).toBe(80);
  });

  it('treats a bad step as one item', () => {
    expect(revealCount(0, 10, 0)).toBe(1);
  });
});

describe('nextRevealCount', () => {
  it('adds one step until everything is shown', () => {
    expect(nextRevealCount(50, 1000, 50)).toBe(100);
    expect(nextRevealCount(980, 1000, 50)).toBe(1000);
    expect(nextRevealCount(1000, 1000, 50)).toBe(1000);
  });

  it('grows from the first step even when nothing was counted yet', () => {
    expect(nextRevealCount(0, 1000, 50)).toBe(100);
  });
});
