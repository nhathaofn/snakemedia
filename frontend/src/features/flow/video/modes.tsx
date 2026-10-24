import type { TabItem } from '../../../components/ui';
import { AllModePanel } from './all/AllModePanel';
import { SingleModePanel } from './single/SingleModePanel';

/**
 * Tabs of the Video tools panel, in display order; the first one opens by default.
 * New tab = one folder with its panel + one entry here.
 */
export const VIDEO_MODES = [
  { value: 'single', label: 'Single', content: <SingleModePanel /> },
  { value: 'all', label: 'All', content: <AllModePanel /> },
] as const satisfies readonly TabItem<string>[];
