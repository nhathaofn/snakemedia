import { describe, expect, it } from 'vitest';
import { FEATURE_ROUTES, NAVIGATION_ITEMS } from './registry';

describe('feature route registry', () => {
  it('has unique ids and paths', () => {
    const ids = FEATURE_ROUTES.map((r) => r.id);
    const paths = FEATURE_ROUTES.map((r) => r.path);
    expect(new Set(ids).size).toBe(ids.length);
    expect(new Set(paths).size).toBe(paths.length);
  });

  it('uses absolute paths and defines an index route', () => {
    expect(FEATURE_ROUTES.every((r) => r.path.startsWith('/'))).toBe(true);
    expect(FEATURE_ROUTES.some((r) => r.path === '/')).toBe(true);
  });

  it('derives navigation from visible routes', () => {
    const visible = FEATURE_ROUTES.filter((r) => r.showInNav !== false);
    expect(NAVIGATION_ITEMS.map((n) => n.path)).toEqual(visible.map((r) => r.path));
  });
});
