import { Navigate, useNavigate, useRouteError } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { AppLayout } from '../components/layout';
import { Button } from '../components/ui';
import { APP_SECTIONS, DEFAULT_PATH, defaultFeaturePath } from './registry';
import type { AppSection } from '../types';
import './RouteError.css';

export function RouteErrorFallback() {
  const error = useRouteError();
  const navigate = useNavigate();
  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
        ? error
        : 'Đã xảy ra lỗi không mong muốn trên trang này.';
  return (
    <div role="alert" className="route-error">
      <h2 className="route-error-title">Đã xảy ra lỗi khi tải trang</h2>
      <p className="route-error-message">{message}</p>
      <div className="route-error-actions">
        <Button variant="primary" onClick={() => window.location.reload()}>
          Tải lại trang
        </Button>
        <Button variant="ghost" onClick={() => navigate(DEFAULT_PATH)}>
          Về trang chủ
        </Button>
      </div>
    </div>
  );
}

/**
 * '/home' → { path: 'home', children: [index → first feature, 'accounts', 'proxy', …] }.
 * Case-sensitive so '/Home/...' falls through to the fallback instead of rendering a
 * page whose section the header and sidebar cannot resolve.
 */
function toRouteObject(section: AppSection): RouteObject {
  return {
    path: section.path.replace(/^\//, ''),
    caseSensitive: true,
    errorElement: <RouteErrorFallback />,
    children: [
      { index: true, element: <Navigate to={defaultFeaturePath(section)} replace /> },
      ...section.features.map((f) => ({
        path: f.path,
        caseSensitive: true,
        element: f.element,
        errorElement: <RouteErrorFallback />,
      })),
    ],
  };
}

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppLayout />,
    errorElement: <RouteErrorFallback />,
    children: [
      { index: true, element: <Navigate to={DEFAULT_PATH} replace /> },
      ...APP_SECTIONS.map(toRouteObject),
      { path: '*', element: <Navigate to={DEFAULT_PATH} replace /> },
    ],
  },
];
