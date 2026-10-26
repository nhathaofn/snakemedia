import { Navigate, useRouteError } from 'react-router-dom';
import type { RouteObject } from 'react-router-dom';
import { AppLayout } from '../components/layout';
import { Button } from '../components/ui';
import { APP_SECTIONS, DEFAULT_PATH, defaultFeaturePath } from './registry';
import type { AppSection } from '../types';

export function RouteErrorFallback() {
  const error = useRouteError();
  const message =
    error instanceof Error
      ? error.message
      : typeof error === 'string'
        ? error
        : 'Đã xảy ra lỗi không mong muốn trên trang này.';
  return (
    <div
      role="alert"
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '60vh',
        padding: '2rem',
        textAlign: 'center',
        gap: '1rem',
      }}
    >
      <h2 style={{ fontSize: '1.25rem', fontWeight: 600 }}>Đã xảy ra lỗi khi tải trang</h2>
      <p style={{ color: 'var(--color-text-danger, #ef4444)', maxWidth: '500px', margin: 0 }}>
        {message}
      </p>
      <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.5rem' }}>
        <Button variant="primary" onClick={() => window.location.reload()}>
          Tải lại trang
        </Button>
        <Button variant="ghost" onClick={() => (window.location.href = '/')}>
          Về trang chủ
        </Button>
      </div>
    </div>
  );
}

/**
 * '/home' → { path: 'home', children: [index → first feature, 'overview', 'accounts', …] }.
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
