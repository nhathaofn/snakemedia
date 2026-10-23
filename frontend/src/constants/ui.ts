import { Monitor, Moon, Sun, Worm } from 'lucide-react';
import { Theme } from '../types';
import type { IconComponent } from '../types';
import type { SegmentedOption } from '../components/ui/SegmentedControl/SegmentedControl';

/** Default auto-dismiss delay for toasts; 0 keeps a toast until dismissed. */
export const DEFAULT_TOAST_DURATION_MS = 4000;

/** Media query used to resolve the `system` theme. */
export const PREFERS_DARK_QUERY = '(prefers-color-scheme: dark)';

/** Brand mark shown in the title bar. */
export const APP_ICON: IconComponent = Worm;

/** Theme choices in display order. */
export const THEME_OPTIONS: readonly SegmentedOption<Theme>[] = [
  { value: Theme.Light, label: 'Light', icon: Sun },
  { value: Theme.Dark, label: 'Dark', icon: Moon },
  { value: Theme.System, label: 'System', icon: Monitor },
];
