import { useEffect } from 'react';
import { useAppStore } from '../stores';

/** The token holding the window width below which the layout turns compact. */
export const COMPACT_WIDTH_TOKEN = '--layout-compact-width';

/** Reads a px token from :root; NaN when it is missing. */
export function readPxToken(name: string, root: Element = document.documentElement): number {
  return Number.parseFloat(getComputedStyle(root).getPropertyValue(name));
}

/**
 * Marks the root with data-layout="compact" while the window is narrower than
 * the --layout-compact-width token (CSS media queries cannot read tokens), and
 * collapses the sidebar when the window becomes narrow. The user can open the
 * sidebar again; it is only collapsed on the change, not held collapsed.
 */
export function useCompactLayout() {
  const setSidebarCollapsed = useAppStore((s) => s.setSidebarCollapsed);

  useEffect(() => {
    const root = document.documentElement;
    const width = readPxToken(COMPACT_WIDTH_TOKEN, root);
    if (!Number.isFinite(width) || typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(`(max-width: ${width}px)`);
    const apply = (compact: boolean) => {
      root.dataset.layout = compact ? 'compact' : 'wide';
      if (compact) setSidebarCollapsed(true);
    };
    apply(query.matches);
    const onChange = (event: MediaQueryListEvent) => apply(event.matches);
    query.addEventListener('change', onChange);
    return () => query.removeEventListener('change', onChange);
  }, [setSidebarCollapsed]);
}
