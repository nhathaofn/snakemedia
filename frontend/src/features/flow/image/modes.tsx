import type { TabItem } from '../../../components/ui';
import { AllModePanel } from './all/AllModePanel';
import { SingleModePanel } from './single/SingleModePanel';

/**
 * Tabs of the Image tools panel, in display order; the first one opens by default.
 * New tab = one folder with its panel + one entry here.
 */
export const IMAGE_MODES = [
  { value: 'single', label: 'Đơn', content: <SingleModePanel /> },
  { value: 'all', label: 'Hàng loạt', content: <AllModePanel /> },
] as const satisfies readonly TabItem<string>[];
