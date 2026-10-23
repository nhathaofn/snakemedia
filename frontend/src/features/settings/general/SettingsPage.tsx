import React from 'react';
import './SettingsPage.css';
import { Card, SegmentedControl } from '../../../components/ui';
import { THEME_OPTIONS } from '../../../constants';
import { useConfigUpdate } from '../../../hooks';
import { useAppStore } from '../../../stores';

/**
 * SettingsPage - user preferences backed by the Go config (internal/config).
 */
export const SettingsPage: React.FC = () => {
  const config = useAppStore((s) => s.config);
  const { update, saving } = useConfigUpdate();

  return (
    <div className="page-shell settings-page">
      <h1 className="settings-title">Settings</h1>

      <section className="settings-section">
        <h2 className="settings-section-title">Appearance</h2>
        <Card className="settings-card">
          <div className="settings-row">
            <div className="settings-row-text">
              <div className="settings-row-label">Theme</div>
              <div className="settings-row-hint">
                System follows your operating system&apos;s light or dark mode.
              </div>
            </div>
            <SegmentedControl
              aria-label="Theme"
              options={THEME_OPTIONS}
              value={config?.theme}
              onChange={(theme) => void update({ theme })}
              disabled={!config || saving}
            />
          </div>
        </Card>
      </section>
    </div>
  );
};
