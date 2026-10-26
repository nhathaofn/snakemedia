import React from 'react';
import './SettingsPage.css';
import { Card, SegmentedControl } from '../../../components/ui';
import { LOG_LEVEL_OPTIONS, THEME_OPTIONS, WINDOW_SCALE_OPTIONS } from '../../../constants';
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
      <h1 className="settings-title">Cài đặt</h1>

      <section className="settings-section">
        <h2 className="settings-section-title">Giao diện</h2>
        <Card className="settings-card">
          <div className="settings-row">
            <div className="settings-row-text">
              <div className="settings-row-label">Chủ đề</div>
              <div className="settings-row-hint">
                Hệ thống: theo chế độ sáng hoặc tối của hệ điều hành.
              </div>
            </div>
            <SegmentedControl
              aria-label="Chủ đề"
              options={THEME_OPTIONS}
              value={config?.theme}
              onChange={(theme) => void update({ theme })}
              disabled={!config || saving}
            />
          </div>
          <div className="settings-row">
            <div className="settings-row-text">
              <div className="settings-row-label">Kích thước cửa sổ</div>
              <div className="settings-row-hint">
                Phần màn hình chính cửa sổ chiếm khi mở app; áp dụng từ lần mở sau.
              </div>
            </div>
            <SegmentedControl
              aria-label="Kích thước cửa sổ"
              options={WINDOW_SCALE_OPTIONS}
              value={config ? String(config.windowScale) : undefined}
              onChange={(scale) => void update({ windowScale: Number(scale) })}
              disabled={!config || saving}
            />
          </div>
        </Card>
      </section>

      <section className="settings-section">
        <h2 className="settings-section-title">Chẩn đoán</h2>
        <Card className="settings-card">
          <div className="settings-row">
            <div className="settings-row-text">
              <div className="settings-row-label">Mức log</div>
              <div className="settings-row-hint">
                Mức chi tiết ghi vào tệp log; dùng Gỡ lỗi khi cần báo lỗi Flow.
              </div>
            </div>
            <SegmentedControl
              aria-label="Mức log"
              options={LOG_LEVEL_OPTIONS}
              value={config?.logLevel}
              onChange={(logLevel) => void update({ logLevel })}
              disabled={!config || saving}
            />
          </div>
        </Card>
      </section>
    </div>
  );
};
