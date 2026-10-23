import { Navigate } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { AppLayout } from '../components/layout';
import { FEATURE_ROUTES } from './registry';
import type { FeatureRoute } from './registry';

function toRouteObject(route: FeatureRoute): RouteObject {
  if (route.path === '/') {
    return { index: true, element: route.element };
  }
  return { path: route.path.replace(/^\//, ''), element: route.element };
}

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppLayout />,
    children: [
      ...FEATURE_ROUTES.map(toRouteObject),
      { path: '*', element: <Navigate to="/" replace /> },
    ],
  },
];
