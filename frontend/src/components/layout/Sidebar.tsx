import React from 'react';
import { NavLink } from 'react-router-dom';
import { ChevronsLeft } from 'lucide-react';
import './Sidebar.css';
// Import routes modules directly: routes/index depends on layout, so going through it would be circular.
import { SECTION_NAVIGATION, SIDEBAR_PINNED } from '../../routes/registry';
import { useActiveSection } from '../../routes/useActiveSection';
import { useAppStore } from '../../stores';
import type { IconComponent } from '../../types';
import { Icon } from '../ui';

interface SidebarLinkProps {
  to: string;
  label: string;
  icon: IconComponent;
  collapsed: boolean;
}

/** One sidebar row; icon-only with a tooltip while the sidebar is collapsed. */
const SidebarLink: React.FC<SidebarLinkProps> = ({ to, label, icon, collapsed }) => (
  <NavLink
    to={to}
    className={({ isActive }) => `sidebar-link ${isActive ? 'active' : ''}`}
    title={collapsed ? label : undefined}
    aria-label={collapsed ? label : undefined}
  >
    <Icon icon={icon} size="lg" />
    {!collapsed && <span className="sidebar-label">{label}</span>}
  </NavLink>
);

/**
 * Lists the features of the section selected in the header. Sections placed in the
 * sidebar (Settings) stay pinned at the bottom, just above the version and collapse toggle.
 */
export const Sidebar: React.FC = () => {
  const section = useActiveSection();
  const items = (section && SECTION_NAVIGATION.get(section.id)) ?? [];
  const sidebarCollapsed = useAppStore((s) => s.sidebarCollapsed);
  const toggleSidebar = useAppStore((s) => s.toggleSidebar);
  const version = useAppStore((s) => s.appInfo?.version);
  const toggleLabel = sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar';

  return (
    <aside className={`sidebar ${sidebarCollapsed ? 'collapsed' : ''}`}>
      <nav className="sidebar-nav" aria-label={section?.label}>
        {items.map((item) => (
          <SidebarLink
            key={item.id}
            to={item.path}
            label={item.label}
            icon={item.icon}
            collapsed={sidebarCollapsed}
          />
        ))}
      </nav>
      {SIDEBAR_PINNED.length > 0 && (
        <nav className="sidebar-pinned" aria-label="Pinned">
          {SIDEBAR_PINNED.map((pinned) => (
            <SidebarLink
              key={pinned.id}
              to={pinned.path}
              label={pinned.label}
              icon={pinned.icon}
              collapsed={sidebarCollapsed}
            />
          ))}
        </nav>
      )}
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
