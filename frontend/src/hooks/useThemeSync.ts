import { useEffect } from 'react';
import { PREFERS_DARK_QUERY } from '../constants';
import { useAppStore } from '../stores';
import { resolveTheme } from '../utils';

/**
 * Applies the configured theme to <html data-theme>, following the OS
 * preference live when the theme is `system`.
 */
export function useThemeSync(): void {
  const theme = useAppStore((s) => s.config?.theme);

  useEffect(() => {
    if (!theme) return;
    const media = window.matchMedia(PREFERS_DARK_QUERY);
    const apply = () => {
      document.documentElement.setAttribute('data-theme', resolveTheme(theme, media.matches));
    };
    apply();
    media.addEventListener('change', apply);
    return () => media.removeEventListener('change', apply);
  }, [theme]);
}
