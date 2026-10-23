import type { TabItem } from '../../../components/ui';
import { AllModePanel } from './all/AllModePanel';
import { SingleModePanel } from './single/SingleModePanel';

/**
 * Tabs of the Image tools panel, in display order; the first one opens by default.
 * New tab = one folder with its panel + one entry here.
 */
export const IMAGE_MODES = [
  { value: 'single', label: 'Single', content: <SingleModePanel /> },
  { value: 'all', label: 'All', content: <AllModePanel /> },
] as const satisfies readonly TabItem<string>[];

export type ImageMode = (typeof IMAGE_MODES)[number]['value'];

export const DEFAULT_IMAGE_MODE: ImageMode = IMAGE_MODES[0].value;
