import type { ReactNode } from 'react';
import { House, Settings } from 'lucide-react';
import { HomePage } from '../features/home/HomePage';
import { SettingsPage } from '../features/settings/SettingsPage';
import type { IconComponent, NavigationItem } from '../types';

/** A top-level screen rendered inside AppLayout. */
export interface FeatureRoute {
  id: string;
  /** Absolute path; '/' is the index route. */
  path: string;
  label: string;
  icon: IconComponent;
  element: ReactNode;
  /** Defaults to true. */
  showInNav?: boolean;
}

/**
 * Single source of truth for screens. Adding a feature = adding one entry here;
 * both the router and the sidebar are derived from it.
 */
export const FEATURE_ROUTES: FeatureRoute[] = [
  { id: 'home', path: '/', label: 'Home', icon: House, element: <HomePage /> },
  {
    id: 'settings',
    path: '/settings',
    label: 'Settings',
    icon: Settings,
    element: <SettingsPage />,
  },
];

export const NAVIGATION_ITEMS: NavigationItem[] = FEATURE_ROUTES.filter(
  (r) => r.showInNav !== false,
).map(({ id, label, path, icon }) => ({ id, label, path, icon }));
