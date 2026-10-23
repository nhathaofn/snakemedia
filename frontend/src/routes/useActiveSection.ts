import { useLocation } from 'react-router-dom';
import { findFeature, findSection } from './registry';
import type { AppSection, FeatureRoute } from '../types';

/**
 * Section owning the current URL. Lives in routes/ rather than hooks/ because the
 * registry imports feature modules, which themselves import hooks/.
 */
export function useActiveSection(): AppSection | undefined {
  const { pathname } = useLocation();
  return findSection(pathname);
}

/** Feature owning the current URL. Same placement rationale as `useActiveSection`. */
export function useActiveFeature(): FeatureRoute | undefined {
  const { pathname } = useLocation();
  return findFeature(pathname);
}
