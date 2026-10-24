import { describe, expect, it } from 'vitest';
import { cssAspectRatio, ratioLabel } from './format';

describe('format', () => {
  it('labels ratios by orientation', () => {
    expect(ratioLabel('16:9')).toBe('16:9 · Ngang');
    expect(ratioLabel('3:4')).toBe('3:4 · Dọc');
    expect(ratioLabel('1:1')).toBe('1:1 · Vuông');
    expect(ratioLabel('bad')).toBe('bad');
    expect(cssAspectRatio('9:16')).toBe('9 / 16');
  });
});
