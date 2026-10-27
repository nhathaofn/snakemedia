import React from 'react';
import { FolderOpen } from 'lucide-react';
import { Button, Icon, SegmentedControl } from '../../../components/ui';
import {
  NumberSetting,
  SettingRow,
  SettingsSection,
  SettingsShell,
  TextSetting,
  ToggleSetting,
} from '../../../components/settings';
import { useConfigSection } from '../../../hooks';
import { api, unwrap } from '../../../services';
import { useAppStore } from '../../../stores';
import { MuseMode } from '../../../types';

const modeOptions = [
  { value: MuseMode.Rotate, label: 'Luân phiên' },
  { value: MuseMode.Sequential, label: 'Tuần tự' },
] as const;

/**
 * How the app runs its Muse queue. What Muse itself allows (attachments,
 * quota, models) is read from muse.ai on each account and is not set here;
 * account browsers and proxies are shared (Cài đặt, Trang chủ).
 */
export const MuseSettingsPage: React.FC = () => {
  const { section: muse, patch, saving } = useConfigSection('muse');
  const addToast = useAppStore((s) => s.addToast);

  if (!muse) {
    return <SettingsShell title="Cài đặt Muse">Đang tải cài đặt…</SettingsShell>;
  }

  const pickFolder = async () => {
    try {
      const res = unwrap(await api.chooseImageOutputDirectory(muse.outputDir));
      if (!res.canceled && res.path) patch({ outputDir: res.path });
    } catch (cause) {
      addToast({ type: 'error', message: cause instanceof Error ? cause.message : String(cause) });
    }
  };

  return (
    <SettingsShell title="Cài đặt Muse">
      <SettingsSection
        title="Hàng đợi"
        advanced={
          <>
            <SettingRow label="Số lần gửi tối đa" hint="Một tác vụ được gửi lại tối đa (1–5).">
              <NumberSetting
                aria-label="Số lần gửi tối đa"
                value={muse.maxAttempts}
                min={1}
                max={5}
                disabled={saving}
                onCommit={(maxAttempts) => patch({ maxAttempts })}
              />
            </SettingRow>
            <SettingRow
              label="Giãn cách giữa hai lần gửi"
              hint="Ngẫu nhiên trong khoảng, trên cùng tài khoản."
            >
              <NumberSetting
                aria-label="Giãn cách tối thiểu"
                value={muse.sendGapMinSeconds}
                min={0}
                max={muse.sendGapMaxSeconds}
                disabled={saving}
                onCommit={(sendGapMinSeconds) => patch({ sendGapMinSeconds })}
              />
              <NumberSetting
                aria-label="Giãn cách tối đa"
                value={muse.sendGapMaxSeconds}
                min={muse.sendGapMinSeconds}
                max={600}
                unit="giây"
                disabled={saving}
                onCommit={(sendGapMaxSeconds) => patch({ sendGapMaxSeconds })}
              />
            </SettingRow>
            <SettingRow label="Số tác vụ giữ trong lịch sử">
              <NumberSetting
                aria-label="Số tác vụ giữ trong lịch sử"
                value={muse.maxJobs}
                min={10}
                max={10000}
                disabled={saving}
                onCommit={(maxJobs) => patch({ maxJobs })}
              />
            </SettingRow>
          </>
        }
      >
        <SettingRow
          label="Chia việc cho tài khoản"
          hint="Luân phiên: ưu tiên tài khoản rảnh, còn nhiều hạn mức. Tuần tự: theo thứ tự đã lưu."
        >
          <SegmentedControl
            aria-label="Chia việc cho tài khoản"
            options={modeOptions}
            value={muse.mode}
            disabled={saving}
            onChange={(mode) => patch({ mode })}
          />
        </SettingRow>
        <SettingRow label="Tài khoản chạy cùng lúc">
          <NumberSetting
            aria-label="Tài khoản chạy cùng lúc"
            value={muse.maxAccounts}
            min={1}
            max={50}
            disabled={saving}
            onCommit={(maxAccounts) => patch({ maxAccounts })}
          />
        </SettingRow>
        <SettingRow label="Luồng" hint="Tổng số tác vụ chạy cùng lúc / trên mỗi tài khoản.">
          <NumberSetting
            aria-label="Tổng số luồng"
            value={muse.totalThreads}
            min={1}
            max={100}
            disabled={saving}
            onCommit={(totalThreads) => patch({ totalThreads })}
          />
          <NumberSetting
            aria-label="Luồng mỗi tài khoản"
            value={muse.threadsPerAccount}
            min={1}
            max={5}
            unit="/ tài khoản"
            disabled={saving}
            onCommit={(threadsPerAccount) => patch({ threadsPerAccount })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection
        title="Hạn mức"
        advanced={
          <SettingRow
            label="Nghỉ khi hết lượt"
            hint="Khi Muse báo chạm giới hạn mà chưa đọc được giờ hạn mức tuần đặt lại."
          >
            <NumberSetting
              aria-label="Nghỉ khi hết lượt"
              value={muse.usagePauseMinutes}
              min={1}
              max={10080}
              unit="phút"
              disabled={saving}
              onCommit={(usagePauseMinutes) => patch({ usagePauseMinutes })}
            />
          </SettingRow>
        }
      >
        <SettingRow
          label="Dùng token nạp thêm"
          hint="Khi hết hạn mức tuần, tài khoản còn token nạp thêm vẫn chạy tiếp; tắt thì nghỉ tới khi hạn mức tuần đặt lại."
        >
          <ToggleSetting
            aria-label="Dùng token nạp thêm"
            value={muse.useTopup}
            disabled={saving}
            onChange={(useTopup) => patch({ useTopup })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection
        title="Thời gian chờ"
        advanced={
          <SettingRow
            label="Thời gian chờ một lần gửi"
            hint="Gồm cả lúc Muse chuẩn bị máy và lần đầu đọc giới hạn của Muse."
          >
            <NumberSetting
              aria-label="Thời gian chờ một lần gửi"
              value={muse.sendTimeoutSeconds}
              min={30}
              max={600}
              unit="giây"
              disabled={saving}
              onCommit={(sendTimeoutSeconds) => patch({ sendTimeoutSeconds })}
            />
          </SettingRow>
        }
      >
        <SettingRow label="Tạo ảnh">
          <NumberSetting
            aria-label="Thời gian chờ tạo ảnh"
            value={muse.imageTimeoutMinutes}
            min={1}
            max={60}
            unit="phút"
            disabled={saving}
            onCommit={(imageTimeoutMinutes) => patch({ imageTimeoutMinutes })}
          />
        </SettingRow>
        <SettingRow label="Tạo video">
          <NumberSetting
            aria-label="Thời gian chờ tạo video"
            value={muse.videoTimeoutMinutes}
            min={5}
            max={120}
            unit="phút"
            disabled={saving}
            onCommit={(videoTimeoutMinutes) => patch({ videoTimeoutMinutes })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection title="Lưu file">
        <SettingRow label="Tự lưu ra thư mục" hint="Chép mỗi file xong vào thư mục theo ngày.">
          <ToggleSetting
            aria-label="Tự lưu ra thư mục"
            value={muse.autoSave}
            disabled={saving}
            onChange={(autoSave) => patch({ autoSave })}
          />
        </SettingRow>
        <SettingRow label="Thư mục lưu" hint="Để trống: thư mục muse-output trong dữ liệu của app.">
          <TextSetting
            aria-label="Thư mục lưu"
            value={muse.outputDir}
            placeholder="Mặc định của app"
            disabled={saving}
            onCommit={(outputDir) => patch({ outputDir })}
          />
          <Button size="sm" disabled={saving} onClick={pickFolder}>
            <Icon icon={FolderOpen} size="sm" />
            <span>Chọn</span>
          </Button>
        </SettingRow>
      </SettingsSection>
    </SettingsShell>
  );
};
