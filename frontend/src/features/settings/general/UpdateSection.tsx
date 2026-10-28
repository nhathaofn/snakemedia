import React, { useCallback, useEffect, useState } from 'react';
import { Button, Modal } from '../../../components/ui';
import { SettingRow, SettingsSection, ToggleSetting } from '../../../components/settings';
import { useConfigSection, useWailsEvent } from '../../../hooks';
import { api } from '../../../services';
import { useAppStore } from '../../../stores';
import { AppEvent } from '../../../types';
import type { UpdateProgress } from '../../../types';
import { formatBytes } from '../../../utils';
import './UpdateSection.css';

function progressText(p: UpdateProgress): string {
  if (p.total <= 0) return 'Đang tải bản cập nhật…';
  const percent = Math.floor((p.done / p.total) * 100);
  return `Đang tải: ${formatBytes(p.done, 1)} / ${formatBytes(p.total, 1)} (${percent}%)`;
}

/**
 * Cập nhật: the running version, a check against the latest release and its
 * silent install (internal/updater). Installing closes the app; every queue
 * keeps its jobs and the installer starts the app again.
 */
export const UpdateSection: React.FC = () => {
  const appInfo = useAppStore((s) => s.appInfo);
  const release = useAppStore((s) => s.availableUpdate);
  const setRelease = useAppStore((s) => s.setAvailableUpdate);
  const addToast = useAppStore((s) => s.addToast);
  const { section: updates, patch, saving } = useConfigSection('updates');
  const [checking, setChecking] = useState(false);
  const [checkedNothing, setCheckedNothing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [progress, setProgress] = useState<UpdateProgress | null>(null);

  useWailsEvent<UpdateProgress>(AppEvent.UpdateProgress, setProgress);

  // A download started before the screen was opened keeps showing.
  useEffect(() => {
    void api.getUpdateProgress().then((res) => {
      if (res.success && res.data && res.data.total > 0) setProgress(res.data);
    });
  }, []);

  const check = useCallback(async () => {
    setChecking(true);
    setCheckedNothing(false);
    const res = await api.checkForUpdate();
    setChecking(false);
    if (!res.success || !res.data) {
      addToast({ type: 'error', title: 'Không kiểm tra được cập nhật', message: res.error ?? '' });
      return;
    }
    setRelease(res.data.available ? res.data : null);
    setCheckedNothing(!res.data.available);
  }, [addToast, setRelease]);

  const install = useCallback(async () => {
    setConfirming(false);
    setProgress({ done: 0, total: 0 });
    const res = await api.installUpdate();
    if (!res.success) {
      setProgress(null);
      addToast({ type: 'error', title: 'Không cài được bản cập nhật', message: res.error ?? '' });
      return;
    }
    addToast({ type: 'info', message: 'Đang đóng app để cài bản mới; app sẽ tự mở lại.' });
  }, [addToast]);

  const downloading = progress !== null;
  let status = `Đang dùng bản ${appInfo?.version ?? '…'}.`;
  if (release) status = `Có bản mới ${release.latest} (đang dùng ${release.current}).`;
  else if (checkedNothing) status += ' Đây là bản mới nhất.';

  return (
    <SettingsSection title="Cập nhật">
      <SettingRow label="Phiên bản" hint={downloading ? progressText(progress) : status}>
        <div className="update-actions">
          <Button
            size="sm"
            onClick={() => void check()}
            isLoading={checking}
            disabled={downloading}
          >
            Kiểm tra cập nhật
          </Button>
          {release && (
            <Button
              size="sm"
              variant="primary"
              onClick={() => setConfirming(true)}
              disabled={!release.canInstall || downloading || checking}
              title={release.canInstall ? undefined : release.reason}
            >
              Cài bản {release.latest}
            </Button>
          )}
        </div>
      </SettingRow>
      {release && (
        <SettingRow label="Có gì mới" stacked>
          {!release.canInstall && release.reason && (
            <p className="update-status">{release.reason}</p>
          )}
          <p className="update-notes">{release.notes || 'Bản phát hành không có ghi chú.'}</p>
        </SettingRow>
      )}
      {updates && (
        <SettingRow
          label="Tự kiểm tra khi mở app"
          hint="Chỉ báo khi có bản mới; việc cài luôn chờ bạn bấm."
        >
          <ToggleSetting
            aria-label="Tự kiểm tra khi mở app"
            value={updates.checkOnStartup}
            disabled={saving}
            onChange={(checkOnStartup) => patch({ checkOnStartup })}
          />
        </SettingRow>
      )}
      {confirming && release && (
        <Modal
          title={`Cài bản ${release.latest}?`}
          onClose={() => setConfirming(false)}
          footer={
            <>
              <Button onClick={() => setConfirming(false)}>Để sau</Button>
              <Button variant="primary" onClick={() => void install()}>
                Tải và cài đặt
              </Button>
            </>
          }
        >
          <p className="update-status">
            App sẽ tải bộ cài, kiểm tra mã SHA256 rồi tự đóng để cài và mở lại. Tác vụ đang chạy
            được giữ trong hàng đợi và tiếp tục (chỉ kiểm tra lại, không gửi lại) sau khi app mở
            lại.
          </p>
        </Modal>
      )}
    </SettingsSection>
  );
};
