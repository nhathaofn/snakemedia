import { useLocation } from 'react-router-dom';
import { findSection } from './registry';
import type { AppSection } from '../types';

/**
 * Section owning the current URL. Lives in routes/ rather than hooks/ because the
 * registry imports feature modules, which themselves import hooks/.
 */
export function useActiveSection(): AppSection | undefined {
  const { pathname } = useLocation();
  return findSection(pathname);
}
