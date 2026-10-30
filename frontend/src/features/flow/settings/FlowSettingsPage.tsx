import React from 'react';
import {
  NumberSetting,
  SettingRow,
  SettingsSection,
  SettingsShell,
  TextSetting,
  ToggleSetting,
} from '../../../components/settings';
import { useConfigSection } from '../../../hooks';

const MB = 1 << 20;

/**
 * Flow generation settings (config `flow`). RPC discovery budgets and the
 * download cap are app defaults, not something a user tunes, so they have no row.
 */
export const FlowSettingsPage: React.FC = () => {
  const { section: flow, patch: patchFlow, saving } = useConfigSection('flow');

  if (!flow) {
    return <SettingsShell title="Cài đặt Flow">Đang tải cài đặt…</SettingsShell>;
  }

  return (
    <SettingsShell title="Cài đặt Flow">
      <SettingsSection
        title="Tạo ảnh và video"
        advanced={
          <>
            <SettingRow label="Nhịp kiểm tra video">
              <NumberSetting
                aria-label="Nhịp kiểm tra video"
                value={flow.videoPollIntervalMs}
                min={1000}
                max={60000}
                step={500}
                unit="ms"
                disabled={saving}
                onCommit={(videoPollIntervalMs) => patchFlow({ videoPollIntervalMs })}
              />
            </SettingRow>
            <SettingRow label="Số lần kiểm tra lỗi liên tiếp">
              <NumberSetting
                aria-label="Số lần kiểm tra lỗi liên tiếp"
                value={flow.maxPollFailures}
                min={1}
                max={100}
                disabled={saving}
                onCommit={(maxPollFailures) => patchFlow({ maxPollFailures })}
              />
            </SettingRow>
            <SettingRow label="Nhịp kiểm tra tối đa khi chờ lâu">
              <NumberSetting
                aria-label="Nhịp kiểm tra tối đa"
                value={flow.pollMaxIntervalMs}
                min={1000}
                max={60000}
                step={500}
                unit="ms"
                disabled={saving}
                onCommit={(pollMaxIntervalMs) => patchFlow({ pollMaxIntervalMs })}
              />
            </SettingRow>
            <SettingRow
              label="Số lần thấy đăng xuất"
              hint="Số lần đọc trang Flow liên tiếp không thấy phiên mới coi là đã đăng xuất."
            >
              <NumberSetting
                aria-label="Số lần thấy đăng xuất"
                value={flow.signedOutReads}
                min={1}
                max={20}
                disabled={saving}
                onCommit={(signedOutReads) => patchFlow({ signedOutReads })}
              />
            </SettingRow>
            <SettingRow label="Giữ danh sách model" hint="0: luôn đọc lại.">
              <NumberSetting
                aria-label="Giữ danh sách model"
                value={flow.modelCacheSeconds}
                min={0}
                max={86400}
                unit="giây"
                disabled={saving}
                onCommit={(modelCacheSeconds) => patchFlow({ modelCacheSeconds })}
              />
            </SettingRow>
            <SettingRow label="Ảnh tối đa">
              <NumberSetting
                aria-label="Ảnh tối đa"
                value={Math.round(flow.maxImageBytes / MB)}
                min={1}
                max={256}
                unit="MB"
                disabled={saving}
                onCommit={(mb) => patchFlow({ maxImageBytes: mb * MB })}
              />
            </SettingRow>
          </>
        }
      >
        <SettingRow
          label="Tự động xóa watermark"
          hint="Đảo ngược watermark ngôi sao góc phải của Flow (Reverse Alpha Blending) không làm suy giảm chất lượng ảnh."
        >
          <ToggleSetting
            aria-label="Tự động xóa watermark"
            value={flow.autoRemoveWatermark ?? true}
            disabled={saving}
            onChange={(autoRemoveWatermark) => patchFlow({ autoRemoveWatermark })}
          />
        </SettingRow>
        <SettingRow
          label="Tự động xóa watermark video"
          hint="Xóa ngôi sao góc phải của video Flow sau khi tải về. Cần FFmpeg trên máy; không có thì giữ nguyên video."
        >
          <ToggleSetting
            aria-label="Tự động xóa watermark video"
            value={flow.autoRemoveVideoWatermark}
            disabled={saving}
            onChange={(autoRemoveVideoWatermark) => patchFlow({ autoRemoveVideoWatermark })}
          />
        </SettingRow>
        <SettingRow
          label="Yêu cầu cùng lúc mỗi tài khoản"
          hint="Số ảnh/video một tài khoản chạy song song."
        >
          <NumberSetting
            aria-label="Yêu cầu cùng lúc mỗi tài khoản"
            value={flow.maxConcurrentRequestsPerAccount}
            min={1}
            max={10}
            disabled={saving}
            onCommit={(maxConcurrentRequestsPerAccount) =>
              patchFlow({ maxConcurrentRequestsPerAccount })
            }
          />
        </SettingRow>
        <SettingRow label="Thời gian chờ tạo ảnh" hint="Một lần tải lên hoặc tạo ảnh.">
          <NumberSetting
            aria-label="Thời gian chờ tạo ảnh"
            value={flow.generationTimeoutSeconds}
            min={10}
            max={600}
            unit="giây"
            disabled={saving}
            onCommit={(generationTimeoutSeconds) => patchFlow({ generationTimeoutSeconds })}
          />
        </SettingRow>
        <SettingRow label="Thời gian chờ video">
          <NumberSetting
            aria-label="Thời gian chờ video"
            value={Math.round(flow.videoGenerationTimeoutSeconds / 60)}
            min={1}
            max={60}
            unit="phút"
            disabled={saving}
            onCommit={(minutes) => patchFlow({ videoGenerationTimeoutSeconds: minutes * 60 })}
          />
        </SettingRow>
        <SettingRow
          label="Giữ tác vụ chưa xong"
          hint="Tác vụ chưa xác nhận được kiểm tra tiếp trong thời gian này, kể cả sau khi mở lại app."
        >
          <NumberSetting
            aria-label="Giữ tác vụ chưa xong"
            value={flow.jobRetentionHours}
            min={1}
            max={8760}
            unit="giờ"
            disabled={saving}
            onCommit={(jobRetentionHours) => patchFlow({ jobRetentionHours })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection
        title="Hàng đợi ảnh (Ảnh → Hàng loạt)"
        advanced={
          <SettingRow
            label="Giữ tối đa"
            hint="Khi đầy, tác vụ đã xong cũ nhất bị bỏ khỏi danh sách; ảnh đã lưu vẫn còn."
          >
            <NumberSetting
              aria-label="Số tác vụ giữ tối đa"
              value={flow.queueMaxTasks}
              min={50}
              max={20000}
              step={50}
              unit="tác vụ"
              disabled={saving}
              onCommit={(queueMaxTasks) => patchFlow({ queueMaxTasks })}
            />
          </SettingRow>
        }
      >
        <SettingRow label="Số luồng" hint="Số prompt chạy cùng lúc.">
          <NumberSetting
            aria-label="Số luồng hàng đợi ảnh"
            value={flow.queueThreads}
            min={1}
            max={20}
            disabled={saving}
            onCommit={(queueThreads) => patchFlow({ queueThreads })}
          />
        </SettingRow>
        <SettingRow
          label="Chờ giữa hai lần gửi"
          hint="Ngẫu nhiên trong khoảng này; kiểm tra lại job đã gửi không phải chờ."
        >
          <NumberSetting
            aria-label="Thời gian chờ ít nhất"
            value={flow.queueGapMinSeconds}
            min={0}
            max={flow.queueGapMaxSeconds}
            unit="giây"
            disabled={saving}
            onCommit={(queueGapMinSeconds) => patchFlow({ queueGapMinSeconds })}
          />
          <NumberSetting
            aria-label="Thời gian chờ nhiều nhất"
            value={flow.queueGapMaxSeconds}
            min={flow.queueGapMinSeconds}
            max={600}
            unit="giây"
            disabled={saving}
            onCommit={(queueGapMaxSeconds) => patchFlow({ queueGapMaxSeconds })}
          />
        </SettingRow>
      </SettingsSection>

      <SettingsSection
        title="Chỉnh sửa video"
        advanced={
          <>
            <SettingRow label="Số lần tải lên">
              <NumberSetting
                aria-label="Số lần tải lên"
                value={flow.uploadAttempts}
                min={1}
                max={10}
                disabled={saving}
                onCommit={(uploadAttempts) => patchFlow({ uploadAttempts })}
              />
            </SettingRow>
            <SettingRow label="Nghỉ giữa hai lần tải lên">
              <NumberSetting
                aria-label="Nghỉ giữa hai lần tải lên"
                value={flow.uploadRetryDelayMs}
                min={0}
                max={60000}
                step={100}
                unit="ms"
                disabled={saving}
                onCommit={(uploadRetryDelayMs) => patchFlow({ uploadRetryDelayMs })}
              />
            </SettingRow>
          </>
        }
      >
        <SettingRow label="ffprobe" hint="Để trống: tìm trong PATH. Chỉ cần cho WebM hoặc file lạ.">
          <TextSetting
            aria-label="Đường dẫn ffprobe"
            value={flow.probePath}
            placeholder="Tự tìm"
            disabled={saving}
            onCommit={(probePath) => patchFlow({ probePath })}
          />
        </SettingRow>
        <SettingRow label="ffmpeg" hint="Để trống: tìm trong PATH. Cần để xóa watermark video.">
          <TextSetting
            aria-label="Đường dẫn ffmpeg"
            value={flow.ffmpegPath}
            placeholder="Tự tìm"
            disabled={saving}
            onCommit={(ffmpegPath) => patchFlow({ ffmpegPath })}
          />
        </SettingRow>
      </SettingsSection>
    </SettingsShell>
  );
};
