import { lazy } from 'react';
import type { ComponentType } from 'react';

/**
 * A page loaded in its own chunk the first time it renders, so the start bundle
 * carries only the shell. Route elements are wrapped in Suspense (routes.config).
 */
export function lazyPage<P extends object>(
  load: () => Promise<ComponentType<P>>,
): ComponentType<P> {
  return lazy(async () => ({ default: await load() }));
}
