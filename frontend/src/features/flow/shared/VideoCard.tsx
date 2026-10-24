import { AlertCircle, Download, X } from 'lucide-react';
import './VideoCard.css';
import type { GeneratedVideoItem } from '../../../types';
import { Spinner } from '../../../components/ui';
import { cssAspectRatio } from './format';

/** One generated video: player (or the reason it failed), details and save. */
export function VideoCard({
  video,
  onSave,
  onResume,
  onDismiss,
  busy,
  showPrompt = true,
}: {
  video: GeneratedVideoItem;
  onResume?: () => void;
  onDismiss?: () => void;
  busy?: boolean;
  showPrompt?: boolean;
  onSave: (localUrl: string) => void;
}) {
  return (
    <div className="flow-video-card">
      <div className="flow-video-frame" style={{ aspectRatio: cssAspectRatio(video.aspectRatio) }}>
        {video.localUrl ? (
          <video src={video.localUrl} controls preload="metadata" />
        ) : (
          <div
            className={`flow-media-error${video.pending ? ' flow-media-pending' : ''}`}
            role={video.pending ? 'status' : 'alert'}
          >
            {video.pending ? <Spinner size={20} /> : <AlertCircle size={20} />}
            <span>{video.pending ? video.error || 'Đang chờ Flow xử lý.' : video.error}</span>
          </div>
        )}
      </div>
      <div className="flow-video-footer">
        <span className="flow-media-badge">
          {video.modelName} · {video.duration}s · {video.resolution}
        </span>
        {showPrompt && (
          <p className="flow-video-prompt" title={video.prompt}>
            {video.prompt}
          </p>
        )}
        {video.pending && onResume && (
          <button type="button" className="flow-card-action-btn" disabled={busy} onClick={onResume}>
            Kiểm tra tiếp
          </button>
        )}
        {video.error && !video.pending && onDismiss && (
          <button
            type="button"
            className="flow-card-action-btn"
            title="Ẩn kết quả lỗi khỏi lịch sử"
            aria-label="Ẩn kết quả lỗi khỏi lịch sử"
            onClick={onDismiss}
          >
            <X size={14} />
          </button>
        )}
        <button
          type="button"
          className="flow-card-action-btn"
          title="Lưu vào thư mục…"
          disabled={!video.localUrl}
          onClick={() => video.localUrl && onSave(video.localUrl)}
        >
          <Download size={14} />
        </button>
      </div>
    </div>
  );
}
