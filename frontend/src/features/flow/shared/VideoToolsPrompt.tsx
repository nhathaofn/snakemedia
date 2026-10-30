import { useCallback, useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { BrowserOpenURL } from '../../../../wailsjs/runtime/runtime';
import { Button, Modal } from '../../../components/ui';
import { FFMPEG_DOWNLOAD_URL } from '../../../constants';
import { useConfigSection, useWailsEvent } from '../../../hooks';
import { ROUTES } from '../../../routes/registry';
import { api, isWailsRuntime } from '../../../services';
import { useAppStore } from '../../../stores';
import { AppEvent } from '../../../types';
import type { VideoTools } from '../../../types';
import './VideoToolsPrompt.css';

/**
 * Asks for FFmpeg when "Tự động xóa watermark video" is on but ffmpeg or
 * ffprobe is not found: once when the app opens, and again when a downloaded
 * video kept its watermark for that reason. "Để sau" quiets it for this run.
 */
export function VideoToolsPrompt() {
  const { section: flow, patch, saving } = useConfigSection('flow');
  const addToast = useAppStore((s) => s.addToast);
  const navigate = useNavigate();
  const [missing, setMissing] = useState<string[]>([]);
  const [later, setLater] = useState(false);
  const [checking, setChecking] = useState(false);
  const enabled = flow?.autoRemoveVideoWatermark ?? false;

  const check = useCallback(async () => {
    const res = await api.checkVideoTools();
    if (!res.success || !res.data) return null;
    setMissing(res.data.missing);
    return res.data;
  }, []);

  const ffmpegPath = flow?.ffmpegPath;
  const probePath = flow?.probePath;
  useEffect(() => {
    if (!enabled || !isWailsRuntime()) return;
    let current = true;
    void api.checkVideoTools().then((res) => {
      if (current && res.success && res.data) setMissing(res.data.missing);
    });
    return () => {
      current = false;
    };
  }, [enabled, ffmpegPath, probePath]);

  useWailsEvent<VideoTools>(AppEvent.FlowVideoToolsMissing, (tools) => {
    setMissing(tools.missing);
    setLater(false);
  });

  if (!enabled || later || missing.length === 0) return null;

  const recheck = async () => {
    setChecking(true);
    const tools = await check();
    setChecking(false);
    if (tools && tools.missing.length === 0) {
      addToast({
        type: 'success',
        title: 'Đã tìm thấy FFmpeg',
        message: 'Video Flow sẽ được xóa watermark.',
      });
    }
  };

  return (
    <Modal
      title="Chưa tìm thấy FFmpeg"
      onClose={() => setLater(true)}
      footer={
        <div className="video-tools-actions">
          <Button disabled={saving} onClick={() => patch({ autoRemoveVideoWatermark: false })}>
            Tắt xóa watermark video
          </Button>
          <Button onClick={() => setLater(true)}>Để sau</Button>
          <Button disabled={checking} onClick={() => void recheck()}>
            Kiểm tra lại
          </Button>
          <Button variant="primary" onClick={() => BrowserOpenURL(FFMPEG_DOWNLOAD_URL)}>
            Tải FFmpeg
          </Button>
        </div>
      }
    >
      <div className="video-tools-body">
        <p>
          "Tự động xóa watermark video" cần <strong>{missing.join(' và ')}</strong> trên máy. Chưa
          có thì video Flow vẫn tải về bình thường nhưng còn watermark.
        </p>
        <ol className="video-tools-steps">
          <li>
            Bấm "Tải FFmpeg", tải bản <strong>ffmpeg-release-essentials.zip</strong> rồi giải nén.
          </li>
          <li>
            Thêm thư mục <code>bin</code> (có ffmpeg.exe và ffprobe.exe) vào PATH rồi mở lại app,
            hoặc dán đường dẫn ffmpeg.exe vào{' '}
            <button
              type="button"
              className="video-tools-link"
              onClick={() => {
                setLater(true);
                navigate(ROUTES.flow.settings);
              }}
            >
              Cài đặt Flow → ffmpeg
            </button>
            .
          </li>
          <li>Bấm "Kiểm tra lại".</li>
        </ol>
      </div>
    </Modal>
  );
}
