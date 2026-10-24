import { AlertCircle, Download } from 'lucide-react';
import './VideoCard.css';
import type { GeneratedVideoItem } from '../../../types';
import { cssAspectRatio } from './format';

/** Grid class for MediaGallery when it lists video cards. */
export const VIDEO_GRID_CLASS = 'flow-video-grid';

/** One generated video: player (or the reason it failed), details and save. */
export function VideoCard({
  video,
  onSave,
}: {
  video: GeneratedVideoItem;
  onSave: (localUrl: string) => void;
}) {
  return (
    <div className="flow-video-card">
      <div className="flow-video-frame" style={{ aspectRatio: cssAspectRatio(video.aspectRatio) }}>
        {video.localUrl ? (
          <video src={video.localUrl} controls preload="metadata" />
        ) : (
          <div className="flow-media-error" role="alert">
            <AlertCircle size={20} />
            <span>{video.error}</span>
          </div>
        )}
      </div>
      <div className="flow-video-footer">
        <span className="flow-media-badge">
          {video.modelName} · {video.duration}s · {video.resolution}
        </span>
        <p className="flow-video-prompt" title={video.prompt}>
          {video.prompt}
        </p>
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
