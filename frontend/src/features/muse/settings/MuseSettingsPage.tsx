import React from 'react';
import { SegmentedControl } from '../../../components/ui';
import {
  NumberSetting,
  SettingRow,
  SettingsSection,
  SettingsShell,
  TextAreaSetting,
  TextSetting,
  ToggleSetting,
} from '../../../components/settings';
import { useConfigSection } from '../../../hooks';
import { ImageFormat, MuseMode } from '../../../types';
import { CleanChatsButton } from './CleanChatsButton';
import './MuseSettingsPage.css';

const modeOptions = [
  { value: MuseMode.Rotate, label: 'Luân phiên' },
  { value: MuseMode.Sequential, label: 'Tuần tự' },
] as const;

const imageFormatOptions = [
  { value: ImageFormat.PNG, label: 'PNG' },
  { value: ImageFormat.JPG, label: 'JPG' },
  { value: ImageFormat.Original, label: 'Giữ nguyên (WebP)' },
] as const;

/**
 * How the app runs its Muse queue. What Muse itself allows (attachments,
 * quota, models) is read from muse.ai on each account and is not set here;
 * account browsers and proxies are shared (Cài đặt, Trang chủ).
 */
export const MuseSettingsPage: React.FC = () => {
  const { section: muse, patch, saving } = useConfigSection('muse');

  if (!muse) {
    return <SettingsShell title="Cài đặt Muse">Đang tải cài đặt…</SettingsShell>;
  }

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
        <SettingRow
          label="Luồng mỗi tài khoản"
          hint="Mọi tài khoản đã đăng nhập và đang dùng được đều tự nhận việc; tài khoản hết lượt, lỗi proxy hay đăng xuất tự được bỏ qua."
        >
          <NumberSetting
            aria-label="Luồng mỗi tài khoản"
            value={muse.threadsPerAccount}
            min={1}
            max={5}
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

      <SettingsSection title="Lưu file">
        <SettingRow
          label="Định dạng ảnh khi lưu"
          hint="Muse tạo ảnh dạng WebP. Ảnh được chuyển sang định dạng này khi lưu vào thư mục Hàng loạt hoặc bằng nút tải xuống; PNG giữ nguyên chất lượng nhưng nặng hơn, JPG nhẹ hơn. Thư viện trong app vẫn giữ bản gốc."
        >
          <SegmentedControl
            aria-label="Định dạng ảnh khi lưu"
            options={imageFormatOptions}
            value={muse.imageFormat}
            disabled={saving}
            onChange={(imageFormat) => patch({ imageFormat })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection title="Tài khoản">
        <SettingRow
          label="Tin nhắn thiết lập sau khi đăng nhập"
          hint="Gửi một lần duy nhất cho mỗi tài khoản, trong một chat riêng, sau lần đầu bạn đăng nhập tài khoản Muse trong cửa sổ Chrome và app đọc được hạn mức. Muse đã nhận thì không gửi lại, kể cả khi sửa nội dung này; chỉ khi Muse không nhận, lần đăng nhập sau mới gửi lại. Gửi lỗi chỉ được ghi chú trên thẻ tài khoản. Để trống để không gửi."
          stacked
        >
          <TextAreaSetting
            aria-label="Tin nhắn thiết lập sau khi đăng nhập"
            value={muse.setupMessage}
            rows={8}
            placeholder="Không gửi tin nhắn nào"
            disabled={saving}
            onCommit={(setupMessage) => patch({ setupMessage })}
          />
        </SettingRow>
        <SettingRow
          label="Dọn đoạn chat phụ"
          hint="Gửi tin nhắn này trong một chat mới trên mọi tài khoản Muse đã đăng nhập; Muse xoá các đoạn chat khác, chỉ giữ chat vừa gửi. Tài khoản còn tác vụ đang chạy hoặc chờ kiểm tra được bỏ qua để tác vụ không mất chat; trong lúc dọn, tài khoản không nhận tác vụ mới."
          stacked
        >
          <TextSetting
            aria-label="Tin nhắn dọn đoạn chat phụ"
            value={muse.cleanupMessage}
            placeholder="Chưa có tin nhắn"
            disabled={saving}
            onCommit={(cleanupMessage) => patch({ cleanupMessage })}
          />
          <CleanChatsButton disabled={saving || !muse.cleanupMessage.trim()} />
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
    </SettingsShell>
  );
};
