import type { ReactElement } from 'react';
import { matchRoutes } from 'react-router-dom';
import { describe, expect, it } from 'vitest';
import { APP_SECTIONS, DEFAULT_PATH, ROUTES, defaultFeaturePath, featurePath } from './registry';
import { routes } from './routes.config';

/** Deepest route the router would render for a URL. */
const leaf = (pathname: string) => matchRoutes(routes, pathname)?.at(-1)?.route;

/** Target of a <Navigate> element. */
const redirectTarget = (pathname: string) =>
  (leaf(pathname)?.element as ReactElement<{ to: string }> | undefined)?.props.to;

describe('router', () => {
  it('renders each feature at its own URL', () => {
    for (const section of APP_SECTIONS) {
      for (const feature of section.features) {
        // Wrapped in Suspense: pages load in their own chunk.
        const element = leaf(featurePath(section, feature))?.element as
          ReactElement<{ children: unknown }> | undefined;
        expect(element?.props.children).toBe(feature.element);
      }
    }
  });

  it('opens a bare section path on its first visible feature', () => {
    for (const section of APP_SECTIONS) {
      expect(redirectTarget(section.path)).toBe(defaultFeaturePath(section));
    }
  });

  it('opens the app on the accounts page', () => {
    expect(DEFAULT_PATH).toBe(ROUTES.home.accounts);
  });

  it('sends the root and unknown URLs to the default page', () => {
    expect(redirectTarget('/')).toBe(DEFAULT_PATH);
    expect(redirectTarget('/nope')).toBe(DEFAULT_PATH);
    expect(redirectTarget(`${APP_SECTIONS[0].path}/nope`)).toBe(DEFAULT_PATH);
  });

  it('does not match a wrongly cased URL', () => {
    const [section] = APP_SECTIONS;
    const url = featurePath(section, section.features[0]).toUpperCase();
    expect(redirectTarget(url)).toBe(DEFAULT_PATH);
  });
});

describe('ROUTES', () => {
  it('holds the URL of every feature', () => {
    const map = ROUTES as unknown as Record<string, Record<string, string>>;
    for (const section of APP_SECTIONS) {
      for (const feature of section.features) {
        expect(map[section.id][feature.id]).toBe(featurePath(section, feature));
      }
    }
  });

  it('is typed with literal paths', () => {
    // Compile-time checks: these fail `tsc` if a feature is renamed or moved.
    const accounts: '/home/accounts' = ROUTES.home.accounts;
    const proxy: '/home/proxy' = ROUTES.home.proxy;
    const flowImage: '/flow/image' = ROUTES.flow.image;
    expect([accounts, proxy, flowImage]).toEqual(['/home/accounts', '/home/proxy', '/flow/image']);
  });
});
