import React from 'react';
import { NavLink } from 'react-router-dom';
import './Header.css';
// Import the registry directly: routes/index depends on layout, so going through it would be circular.
import { HEADER_TABS } from '../../routes/registry';
import { Icon } from '../ui';

/** Section switcher: each tab swaps the sidebar to that section's features. */
export const Header: React.FC = () => {
  return (
    <header className="app-header">
      <nav className="header-tabs" aria-label="Sections">
        {HEADER_TABS.map((section) => (
          <NavLink
            key={section.id}
            to={section.path}
            className={({ isActive }) => `header-tab ${isActive ? 'active' : ''}`}
          >
            <Icon icon={section.icon} size="md" />
            <span className="header-tab-label">{section.label}</span>
          </NavLink>
        ))}
      </nav>
    </header>
  );
};
