import { flowSection } from '../features/flow/section';
import { homeSection } from '../features/home/section';
import { settingsSection } from '../features/settings/section';
import type { AppSection, FeatureRoute, NavigationItem, SectionPlacement } from '../types';

/**
 * Single source of truth for navigation. Each section is a header tab (or a pinned
 * sidebar entry) that owns the sidebar features shown under it; the router, header
 * and sidebar all derive from this list.
 *
 * - New header tab: create `features/<section>/section.tsx` and add it here.
 * - New screen: add one feature to that section's manifest.
 *
 * Display order follows this array within each placement.
 */
const SECTIONS = [homeSection, flowSection, settingsSection] as const;

export const APP_SECTIONS: readonly AppSection[] = SECTIONS;

/** Absolute URL of a feature, e.g. '/home/accounts'. */
export const featurePath = (section: AppSection, feature: FeatureRoute): string =>
  `${section.path}/${feature.path}`;

/** Features listed in the sidebar for a section. */
export const visibleFeatures = (section: AppSection): FeatureRoute[] =>
  section.features.filter((f) => f.showInNav !== false);

/** Where a section opens: its first visible feature (or first feature if all are hidden). */
export function defaultFeaturePath(section: AppSection): string {
  const feature = visibleFeatures(section)[0] ?? section.features[0];
  return featurePath(section, feature);
}

/** Section owning a URL, matched on whole path segments. */
export function findSection(pathname: string): AppSection | undefined {
  return APP_SECTIONS.find((s) => pathname === s.path || pathname.startsWith(`${s.path}/`));
}

/** Where '/' and unknown URLs land: the first section. */
export const DEFAULT_PATH = defaultFeaturePath(APP_SECTIONS[0]);

const placedIn = (placement: SectionPlacement) =>
  APP_SECTIONS.filter((s) => (s.placement ?? 'header') === placement);

export const HEADER_TABS: readonly AppSection[] = placedIn('header');
export const SIDEBAR_PINNED: readonly AppSection[] = placedIn('sidebar');

/** Sidebar entries per section id, resolved to absolute paths once at startup. */
export const SECTION_NAVIGATION: ReadonlyMap<string, readonly NavigationItem[]> = new Map(
  APP_SECTIONS.map((section) => [
    section.id,
    visibleFeatures(section).map((f) => ({
      id: f.id,
      label: f.label,
      icon: f.icon,
      path: featurePath(section, f),
    })),
  ]),
);

type Section = (typeof SECTIONS)[number];

/** Literal URL of every feature, keyed by section id then feature id. */
export type RouteMap = {
  readonly [S in Section as S['id']]: {
    readonly [F in S['features'][number] as F['id']]: `${S['path']}/${F['path']}`;
  };
};

/**
 * Typed URLs for navigation: `navigate(ROUTES.home.accounts)` instead of '/home/accounts'.
 * A renamed or removed feature becomes a compile error at every call site.
 *
 * Feature modules may import this from `routes/registry`, but must only read it while
 * rendering or in handlers: the registry imports the feature modules, so the binding is
 * not yet initialised while they load.
 */
export const ROUTES = Object.fromEntries(
  APP_SECTIONS.map((section) => [
    section.id,
    Object.fromEntries(section.features.map((f) => [f.id, featurePath(section, f)])),
  ]),
) as RouteMap;
