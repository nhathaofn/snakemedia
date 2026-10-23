import { Theme } from '../types';

export type ResolvedTheme = typeof Theme.Dark | typeof Theme.Light;

/** Resolves the `system` preference to a concrete theme. */
export function resolveTheme(theme: Theme, prefersDark: boolean): ResolvedTheme {
  if (theme === Theme.System) return prefersDark ? Theme.Dark : Theme.Light;
  return theme;
}
