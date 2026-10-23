/** Navigation model: header tabs (sections) own the sidebar features shown under them. */
import type { ReactNode } from 'react';
import type { IconComponent } from './app';

/** One screen inside a section, listed in the sidebar while its section is active. */
export interface FeatureRoute {
  id: string;
  /** Single path segment relative to the section, e.g. 'accounts' → '/home/accounts'. */
  path: string;
  label: string;
  icon: IconComponent;
  element: ReactNode;
  /** Defaults to true. Hidden features stay routable but are left out of the sidebar. */
  showInNav?: boolean;
}

/**
 * Where a section's entry is drawn: 'header' is a centred header tab; 'sidebar' is
 * pinned to the bottom of the sidebar and visible from every section.
 */
export type SectionPlacement = 'header' | 'sidebar';

/**
 * A main area of the app: one entry that swaps the sidebar's contents. Manifests are
 * declared `as const satisfies AppSection` so ids and paths stay literal for `ROUTES`.
 */
export interface AppSection {
  id: string;
  /** Absolute single-segment path, e.g. '/home'. */
  path: string;
  label: string;
  icon: IconComponent;
  /** Defaults to 'header'. */
  placement?: SectionPlacement;
  /** The first visible feature is where the section opens. */
  features: readonly FeatureRoute[];
}
