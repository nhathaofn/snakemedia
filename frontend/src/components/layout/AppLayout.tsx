import React, { Fragment } from 'react';
import { Outlet } from 'react-router-dom';
import './AppLayout.css';
import { TitleBar } from './TitleBar';
import { Header } from './Header';
import { Sidebar } from './Sidebar';
import { PreviousRunPrompt, Toaster } from '../feedback';
import { useAppBootstrap, useCompactLayout, useThemeSync, useUpdateNotice } from '../../hooks';
// Import routes modules directly: routes/index depends on layout, so going through it would be circular.
import { useActiveFeature } from '../../routes/useActiveSection';
import { STARTUP_ELEMENTS } from '../../routes/registry';

export const AppLayout: React.FC = () => {
  useAppBootstrap();
  useThemeSync();
  useCompactLayout();
  useUpdateNotice();
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
      {STARTUP_ELEMENTS.map(({ id, element }) => (
        <Fragment key={id}>{element}</Fragment>
      ))}
      <PreviousRunPrompt />
      <Toaster />
    </div>
  );
};
