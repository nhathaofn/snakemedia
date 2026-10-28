import React from 'react';
import { Button, SegmentedControl } from '../../../components/ui';
import {
  NumberSetting,
  SettingRow,
  SettingsSection,
  SettingsShell,
  ToggleSetting,
} from '../../../components/settings';
import { LOG_LEVEL_OPTIONS, THEME_OPTIONS, WINDOW_SCALE_OPTIONS } from '../../../constants';
import { useConfigSection, useConfigUpdate } from '../../../hooks';
import { api } from '../../../services';
import { useAppStore } from '../../../stores';
import { UpdateSection } from './UpdateSection';

const MB = 1 << 20;

/**
 * SettingsPage - app-wide preferences backed by the Go config (internal/config):
 * appearance, the account browsers every provider shares, and diagnostics.
 */
export const SettingsPage: React.FC = () => {
  const config = useAppStore((s) => s.config);
  const { update, saving } = useConfigUpdate();
  const { section: accounts, patch, saving: savingAccounts } = useConfigSection('accounts');
  const busy = saving || savingAccounts;
  const addToast = useAppStore((s) => s.addToast);

  const openLogFolder = async () => {
    const res = await api.openLogFolder();
    if (!res.success)
      addToast({ type: 'error', message: res.error ?? 'Không mở được thư mục log.' });
  };

  return (
    <SettingsShell title="Cài đặt">
      <SettingsSection title="Giao diện">
        <SettingRow label="Chủ đề" hint="Hệ thống: theo chế độ sáng hoặc tối của hệ điều hành.">
          <SegmentedControl
            aria-label="Chủ đề"
            options={THEME_OPTIONS}
            value={config?.theme}
            onChange={(theme) => void update({ theme })}
            disabled={!config || busy}
          />
        </SettingRow>
        <SettingRow
          label="Kích thước cửa sổ"
          hint="Phần màn hình chính cửa sổ chiếm khi mở app; áp dụng từ lần mở sau."
        >
          <SegmentedControl
            aria-label="Kích thước cửa sổ"
            options={WINDOW_SCALE_OPTIONS}
            value={config ? String(config.windowScale) : undefined}
            onChange={(scale) => void update({ windowScale: Number(scale) })}
            disabled={!config || busy}
          />
        </SettingRow>
      </SettingsSection>

      {accounts && (
        <SettingsSection
          title="Trình duyệt tài khoản"
          advanced={
            <>
              <SettingRow label="Chờ trang sẵn sàng" hint="Khi mở trang để kiểm tra đăng nhập.">
                <NumberSetting
                  aria-label="Chờ trang sẵn sàng"
                  value={accounts.pageReadySeconds}
                  min={10}
                  max={600}
                  unit="giây"
                  disabled={busy}
                  onCommit={(pageReadySeconds) => patch({ pageReadySeconds })}
                />
              </SettingRow>
              <SettingRow label="Thời gian chờ một yêu cầu">
                <NumberSetting
                  aria-label="Thời gian chờ một yêu cầu"
                  value={accounts.requestTimeoutSeconds}
                  min={1}
                  max={120}
                  unit="giây"
                  disabled={busy}
                  onCommit={(requestTimeoutSeconds) => patch({ requestTimeoutSeconds })}
                />
              </SettingRow>
              <SettingRow label="Thời gian khởi động Chrome">
                <NumberSetting
                  aria-label="Thời gian khởi động Chrome"
                  value={accounts.browserStartupTimeoutSeconds}
                  min={accounts.requestTimeoutSeconds}
                  max={120}
                  unit="giây"
                  disabled={busy}
                  onCommit={(browserStartupTimeoutSeconds) =>
                    patch({ browserStartupTimeoutSeconds })
                  }
                />
              </SettingRow>
              <SettingRow label="Nhịp kiểm tra trang" hint="Khoảng cách giữa hai lần đọc trang.">
                <NumberSetting
                  aria-label="Nhịp kiểm tra trang"
                  value={accounts.pollIntervalMs}
                  min={500}
                  max={60000}
                  step={100}
                  unit="ms"
                  disabled={busy}
                  onCommit={(pollIntervalMs) => patch({ pollIntervalMs })}
                />
              </SettingRow>
              <SettingRow label="Tự đóng Chrome rảnh sau" hint="0 = không tự đóng.">
                <NumberSetting
                  aria-label="Tự đóng Chrome rảnh sau"
                  value={accounts.idleBrowserMinutes}
                  min={0}
                  max={1440}
                  unit="phút"
                  disabled={busy}
                  onCommit={(idleBrowserMinutes) => patch({ idleBrowserMinutes })}
                />
              </SettingRow>
              <SettingRow
                label="Tệp tải về tối đa"
                hint="Một ảnh hoặc video tải về từ mọi nền tảng."
              >
                <NumberSetting
                  aria-label="Tệp tải về tối đa"
                  value={Math.round(accounts.maxDownloadBytes / MB)}
                  min={1}
                  max={8192}
                  unit="MB"
                  disabled={busy}
                  onCommit={(mb) => patch({ maxDownloadBytes: mb * MB })}
                />
              </SettingRow>
              <SettingRow label="Giới hạn phản hồi">
                <NumberSetting
                  aria-label="Giới hạn phản hồi"
                  value={Math.round(accounts.maxResponseBytes / MB)}
                  min={1}
                  max={32}
                  unit="MB"
                  disabled={busy}
                  onCommit={(mb) => patch({ maxResponseBytes: mb * MB })}
                />
              </SettingRow>
            </>
          }
        >
          <SettingRow
            label="Chạy ẩn Chrome"
            hint="Chrome của tài khoản chạy không hiện cửa sổ khi làm việc; đăng nhập luôn mở cửa sổ."
          >
            <ToggleSetting
              aria-label="Chạy ẩn Chrome"
              value={accounts.headless}
              disabled={busy}
              onChange={(headless) => patch({ headless })}
            />
          </SettingRow>
          <SettingRow label="Thời gian chờ đăng nhập" hint="Cửa sổ đăng nhập chờ tối đa.">
            <NumberSetting
              aria-label="Thời gian chờ đăng nhập"
              value={Math.round(accounts.loginTimeoutSeconds / 60)}
              min={Math.ceil(accounts.browserStartupTimeoutSeconds / 60)}
              max={60}
              unit="phút"
              disabled={busy}
              onCommit={(minutes) => patch({ loginTimeoutSeconds: minutes * 60 })}
            />
          </SettingRow>
        </SettingsSection>
      )}

      <SettingsSection title="Chẩn đoán">
        <SettingRow
          label="Mức log"
          hint="Mức chi tiết ghi vào tệp log; dùng Gỡ lỗi khi cần báo lỗi."
        >
          <SegmentedControl
            aria-label="Mức log"
            options={LOG_LEVEL_OPTIONS}
            value={config?.logLevel}
            onChange={(logLevel) => void update({ logLevel })}
            disabled={!config || busy}
          />
        </SettingRow>
        <SettingRow label="Tệp log" hint="Gửi kèm tệp log khi báo lỗi.">
          <Button size="sm" onClick={() => void openLogFolder()}>
            Mở thư mục log
          </Button>
        </SettingRow>
      </SettingsSection>

      <UpdateSection />
    </SettingsShell>
  );
};
