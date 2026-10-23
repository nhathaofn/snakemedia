import { createHashRouter, RouterProvider } from 'react-router-dom';
import { routes } from './routes.config';

export { FEATURE_ROUTES, NAVIGATION_ITEMS } from './registry';
export type { FeatureRoute } from './registry';

const router = createHashRouter(routes);

export function AppRouter() {
  return <RouterProvider router={router} />;
}
