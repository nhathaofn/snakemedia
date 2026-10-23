import React from 'react';
import { Outlet } from 'react-router-dom';
import './AppLayout.css';
import { TitleBar } from './TitleBar';
import { Sidebar } from './Sidebar';
import { Toaster } from '../feedback';
import { useAppBootstrap, useThemeSync } from '../../hooks';

export const AppLayout: React.FC = () => {
  useAppBootstrap();
  useThemeSync();

  return (
    <div className="app-shell">
      <TitleBar />
      <div className="app-body">
        <Sidebar />
        <main className="app-content">
          <Outlet />
        </main>
      </div>
      <Toaster />
    </div>
  );
};
