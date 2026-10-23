import { Navigate } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { AppLayout } from '../components/layout';
import { APP_SECTIONS, DEFAULT_PATH, defaultFeaturePath } from './registry';
import type { AppSection } from '../types';

/**
 * '/home' → { path: 'home', children: [index → first feature, 'overview', 'accounts', …] }.
 * Case-sensitive so '/Home/...' falls through to the fallback instead of rendering a
 * page whose section the header and sidebar cannot resolve.
 */
function toRouteObject(section: AppSection): RouteObject {
  return {
    path: section.path.replace(/^\//, ''),
    caseSensitive: true,
    children: [
      { index: true, element: <Navigate to={defaultFeaturePath(section)} replace /> },
      ...section.features.map((f) => ({ path: f.path, caseSensitive: true, element: f.element })),
    ],
  };
}

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppLayout />,
    children: [
      { index: true, element: <Navigate to={DEFAULT_PATH} replace /> },
      ...APP_SECTIONS.map(toRouteObject),
      { path: '*', element: <Navigate to={DEFAULT_PATH} replace /> },
    ],
  },
];
