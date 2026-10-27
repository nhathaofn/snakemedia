import { Monitor, Moon, Sun, Worm } from 'lucide-react';
import { LogLevel, Theme } from '../types';
import type { IconComponent } from '../types';
import type { SegmentedOption } from '../components/ui/SegmentedControl/SegmentedControl';

/** Default auto-dismiss delay for toasts; 0 keeps a toast until dismissed. */
export const DEFAULT_TOAST_DURATION_MS = 4000;

/**
 * How long a Flow catalogue or account count is reused between panels that
 * mount together (Single and All tabs), so the same request is not sent twice.
 */
export const FLOW_SHARED_REQUEST_REUSE_MS = 10_000;

/** Media query used to resolve the `system` theme. */
export const PREFERS_DARK_QUERY = '(prefers-color-scheme: dark)';

/** Brand mark shown in the title bar. */
export const APP_ICON: IconComponent = Worm;

/** Theme choices in display order. */
export const THEME_OPTIONS: readonly SegmentedOption<Theme>[] = [
  { value: Theme.Light, label: 'Sáng', icon: Sun },
  { value: Theme.Dark, label: 'Tối', icon: Moon },
  { value: Theme.System, label: 'Hệ thống', icon: Monitor },
];

/** Log levels in display order (most to least detailed). */
export const LOG_LEVEL_OPTIONS: readonly SegmentedOption<LogLevel>[] = [
  { value: LogLevel.Debug, label: 'Gỡ lỗi' },
  { value: LogLevel.Info, label: 'Thông tin' },
  { value: LogLevel.Warn, label: 'Cảnh báo' },
  { value: LogLevel.Error, label: 'Lỗi' },
];

/**
 * Startup window sizes offered in Settings, as a share of the primary screen.
 * They sit inside config.MinWindowScale..MaxWindowScale, which the backend enforces.
 */
export const WINDOW_SCALE_OPTIONS: readonly SegmentedOption<string>[] = [
  0.5, 0.6, 0.7, 0.8, 0.9, 1,
].map((scale) => ({ value: String(scale), label: `${Math.round(scale * 100)}%` }));

/** Items a long list (Muse queue, library) shows per step as the user scrolls. */
export const LIST_REVEAL_STEP = 40;

/** Accounts checked at once by "Kiểm tra tất cả"; each check may start a browser. */
export const ACCOUNT_CHECK_CONCURRENCY = 3;

/** Most variants one Muse Đơn prompt can ask for; each is its own paid Muse chat. */
export const MUSE_SINGLE_MAX_VARIANTS = 4;
