import type { TabItem } from '../../../components/ui';
import { SingleModePanel } from './single/SingleModePanel';

/**
 * Tabs of the Video tools panel, in display order; the first one opens by default.
 * New tab = one folder with its panel + one entry here. A batch tab comes back
 * once video has a backend queue like Ảnh → Hàng loạt.
 */
export const VIDEO_MODES = [
  { value: 'single', label: 'Đơn', content: <SingleModePanel /> },
] as const satisfies readonly TabItem<string>[];
