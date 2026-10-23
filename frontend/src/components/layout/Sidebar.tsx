import React from 'react';
import { NavLink } from 'react-router-dom';
import { ChevronsLeft } from 'lucide-react';
import './Sidebar.css';
// Import the registry directly: routes/index depends on layout, so going through it would be circular.
import { NAVIGATION_ITEMS } from '../../routes/registry';
import { useAppStore } from '../../stores';
import { Icon } from '../ui';

export const Sidebar: React.FC = () => {
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useAppStore((s) => s.toggleSidebar);
  const version = useAppStore((s) => s.appInfo?.version);
  const toggleLabel = sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar';

  return (
    <aside className={`sidebar ${sidebarCollapsed ? 'collapsed' : ''}`}>
      <nav className="sidebar-nav">
        {NAVIGATION_ITEMS.map((item) => (
          <NavLink
            key={item.id}
            to={item.path}
            end={item.path === '/'}
            className={({ isActive }) => `sidebar-link ${isActive ? 'active' : ''}`}
            title={sidebarCollapsed ? item.label : undefined}
            aria-label={sidebarCollapsed ? item.label : undefined}
          >
            <Icon icon={item.icon} size="lg" />
            {!sidebarCollapsed && <span className="sidebar-label">{item.label}</span>}
          </NavLink>
        ))}
      </nav>
      <div className="sidebar-footer">
        {!sidebarCollapsed && version && (
          <span className="sidebar-version" title={`Version ${version}`}>
            v{version}
          </span>
        )}
        <button
          className="sidebar-toggle"
          onClick={toggleSidebar}
          title={toggleLabel}
          aria-label={toggleLabel}
          aria-expanded={!sidebarCollapsed}
        >
          <Icon icon={ChevronsLeft} size="md" className="sidebar-toggle-icon" />
        </button>
      </div>
    </aside>
  );
};
