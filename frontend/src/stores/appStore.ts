import { create } from 'zustand';
import { DEFAULT_TOAST_DURATION_MS } from '../constants';
import type { AppConfig, AppInfo, ToastNotification, UpdateRelease } from '../types';

interface AppState {
  // Backend-sourced state (null until loaded by useAppBootstrap)
  appInfo: AppInfo | null;
  setAppInfo: (info: AppInfo) => void;
  config: AppConfig | null;
  setConfig: (cfg: AppConfig) => void;
  /** Newer release found by the startup check or by Cài đặt → Chung. */
  availableUpdate: UpdateRelease | null;
  setAvailableUpdate: (release: UpdateRelease | null) => void;

  // Sidebar navigation state
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;
  setSidebarCollapsed: (collapsed: boolean) => void;

  // Notification toasts
  toasts: ToastNotification[];
  addToast: (toast: Omit<ToastNotification, 'id'>) => string;
  removeToast: (id: string) => void;
}

export const useAppStore = create<AppState>((set, get) => ({
  appInfo: null,
  setAppInfo: (appInfo) => set({ appInfo }),
  config: null,
  setConfig: (config) => set({ config }),
  availableUpdate: null,
  setAvailableUpdate: (availableUpdate) => set({ availableUpdate }),

  sidebarCollapsed: false,
  toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),

  toasts: [],
  addToast: (toast) => {
    const id = crypto.randomUUID();
    const duration = toast.durationMs ?? DEFAULT_TOAST_DURATION_MS;
    set((state) => ({ toasts: [...state.toasts, { ...toast, id }] }));
    if (duration > 0) {
      setTimeout(() => get().removeToast(id), duration);
    }
    return id;
  },
  removeToast: (id) => set((state) => ({ toasts: state.toasts.filter((t) => t.id !== id) })),
}));
