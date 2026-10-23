import { Settings, SlidersHorizontal } from 'lucide-react';
import type { AppSection } from '../../types';
import { SettingsPage } from './general/SettingsPage';

/** Settings: pinned to the bottom of the sidebar rather than a header tab. */
export const settingsSection = {
  id: 'settings',
  path: '/settings',
  label: 'Settings',
  icon: Settings,
  placement: 'sidebar',
  features: [
    {
      id: 'general',
      path: 'general',
      label: 'General',
      icon: SlidersHorizontal,
      element: <SettingsPage />,
    },
  ],
} as const satisfies AppSection;
