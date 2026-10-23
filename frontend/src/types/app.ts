/** Frontend-only types. Backend DTOs live in ./backend (generated from Go). */
import type { LucideIcon } from 'lucide-react';

/** A monochrome line icon component (lucide-react); render it through ui/Icon. */
export type IconComponent = LucideIcon;

export interface NavigationItem {
  id: string;
  label: string;
  path: string;
  icon: IconComponent;
  badge?: string | number;
}

export type ToastType = 'info' | 'success' | 'warning' | 'error';

export interface ToastNotification {
  id: string;
  type: ToastType;
  title?: string;
  message: string;
  durationMs?: number;
}
