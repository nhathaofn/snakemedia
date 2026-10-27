import React from 'react';
import { Outlet } from 'react-router-dom';
import './AppLayout.css';
import { TitleBar } from './TitleBar';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { Toaster } from '../feedback';
import { useAppBootstrap, useCompactLayout, useThemeSync } from '../../hooks';
// Import routes modules directly: routes/index depends on layout, so going through it would be circular.
import { useActiveFeature } from '../../routes/useActiveSection';

export const AppLayout: React.FC = () => {
  useAppBootstrap();
  useThemeSync();
  useCompactLayout();
  const fullLayout = useActiveFeature()?.layout === 'full';

  return (
    <div className="app-shell">
      <TitleBar />
      <Header />
      <div className="app-body">
        <Sidebar />
        <main className={`app-content ${fullLayout ? 'app-content-full' : ''}`}>
          <Outlet />
        </main>
      </div>
      <Toaster />
    </div>
  );
};
