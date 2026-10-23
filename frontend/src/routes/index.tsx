import { createHashRouter, RouterProvider } from 'react-router-dom';
import { routes } from './routes.config';

export {
  APP_SECTIONS,
  DEFAULT_PATH,
  HEADER_TABS,
  ROUTES,
  SECTION_NAVIGATION,
  SIDEBAR_PINNED,
  defaultFeaturePath,
  featurePath,
  findFeature,
  findSection,
} from './registry';
export type { RouteMap } from './registry';
export { useActiveFeature, useActiveSection } from './useActiveSection';

const router = createHashRouter(routes);

export function AppRouter() {
  return <RouterProvider router={router} />;
}
