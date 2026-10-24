import { useEffect, useRef } from 'react';
import { AlertCircle, FileVideo, RefreshCw } from 'lucide-react';
import { Spinner } from '../../../components/ui';
import { api } from '../../../services/api';
import { useAppStore } from '../../../stores';
import { formatBytes } from '../../../utils';
import { clockLabel, selectedModelOf } from '../shared';
import { MIN_TRIM_SECONDS, useFlowEditStore } from './store/flowEditStore';

const TRIM_STEP_SECONDS = 0.1;

/**
 * Source video of an edit: picked through the native dialog (the backend copies
 * it into the media folder), previewed, and trimmed to the part Flow reads.
 * The preview plays only the trimmed part.
 */
export function SourceVideoPanel() {
  const addToast = useAppStore((s) => s.addToast);
  const source = useFlowEditStore((s) => s.source);
  const setSource = useFlowEditStore((s) => s.setSource);
  const isSourceLoading = useFlowEditStore((s) => s.isSourceLoading);
  const sourceLoadingMessage = useFlowEditStore((s) => s.sourceLoadingMessage);
  const sourceError = useFlowEditStore((s) => s.sourceError);
  const setSourceError = useFlowEditStore((s) => s.setSourceError);
  const uploadedMediaId = useFlowEditStore((s) => s.uploadedMediaId);
  const setUploadedMedia = useFlowEditStore((s) => s.setUploadedMedia);
  const duration = useFlowEditStore((s) => s.sourceDuration);
  const width = useFlowEditStore((s) => s.sourceWidth);
  const height = useFlowEditStore((s) => s.sourceHeight);
  const setMetadata = useFlowEditStore((s) => s.setSourceMetadata);
  const trimStart = useFlowEditStore((s) => s.trimStart);
  const trimEnd = useFlowEditStore((s) => s.trimEnd);
  const setTrim = useFlowEditStore((s) => s.setTrim);
  const generating = useFlowEditStore((s) => s.isGenerating);
  const maxInput = useFlowEditStore(selectedModelOf)?.maxInputSeconds ?? 0;
  const videoRef = useRef<HTMLVideoElement>(null);

  // Show the first frame of the trimmed part whenever its start moves.
  useEffect(() => {
    const video = videoRef.current;
    if (video && duration > 0) video.currentTime = trimStart;
  }, [trimStart, duration]);

  const pick = async () => {
    const res = await api.pickVideo();
    if (!res.success || !res.data) {
      addToast({
        type: 'error',
        title: 'Không chọn được video',
        message: res.error ?? 'Không rõ lỗi.',
      });
      return;
    }
    if (!res.data.canceled) {
      setSource(res.data);
      // Immediately push source video to Google Flow via Scotty uploader
      const uploadRes = await api.uploadSourceVideo(res.data.localUrl);
      if (!uploadRes.success || !uploadRes.data) {
        setSourceError(uploadRes.error ?? 'Không thể tải video lên Google Flow.');
        return;
      }
      setUploadedMedia(uploadRes.data.mediaId, uploadRes.data.width, uploadRes.data.height);
    }
  };

  const keepInTrim = () => {
    const video = videoRef.current;
    if (!video || duration <= 0) return;
    if (video.currentTime >= trimEnd || video.currentTime < trimStart - TRIM_STEP_SECONDS) {
      video.pause();
      video.currentTime = trimStart;
    }
  };

  if (!source) {
    return (
      <div className="flow-field-group">
        <span className="flow-field-label">Video gốc</span>
        <button
          type="button"
          className="reference-dropzone"
          onClick={() => void pick()}
          disabled={generating}
        >
          <FileVideo size={28} className="reference-dropzone-icon" />
          <span className="reference-dropzone-text">Chọn video mp4, mov, webm hoặc m4v</span>
        </button>
      </div>
    );
  }

  const length = trimEnd - trimStart;
  const tooLong = maxInput > 0 && duration > maxInput;

  return (
    <div className="flow-field-group">
      <div className="flow-field-label">
        <span>Video gốc</span>
        <button
          type="button"
          className="flow-edit-link-btn"
          onClick={() => void pick()}
          disabled={generating}
        >
          <RefreshCw size={12} />
          Đổi video
        </button>
      </div>
      <div className="flow-edit-preview">
        {isSourceLoading && (
          <div className="flow-edit-preview-overlay">
            <Spinner size={24} />
            <span>{sourceLoadingMessage}</span>
          </div>
        )}
        {sourceError && (
          <div className="flow-edit-preview-overlay flow-edit-preview-error">
            <AlertCircle size={24} />
            <span>{sourceError}</span>
          </div>
        )}
        <video
          ref={videoRef}
          src={source.localUrl}
          controls={!isSourceLoading && !sourceError}
          preload="metadata"
          onLoadedMetadata={(e) => {
            const { duration: seconds, videoWidth, videoHeight } = e.currentTarget;
            if (Number.isFinite(seconds) && seconds > 0) {
              setMetadata(seconds, videoWidth, videoHeight);
            } else {
              setSourceError('Không thể lấy thời lượng của video.');
            }
          }}
          onError={() => {
            setSourceError('Không thể giải mã hoặc đọc thông số video này.');
          }}
          onTimeUpdate={keepInTrim}
        />
      </div>
      <p className="flow-edit-file" title={source.fileName}>
        {source.fileName} · {formatBytes(source.size, 1)}
        {isSourceLoading && ' · Đang tải lên Flow...'}
        {!isSourceLoading && !sourceError && duration > 0 && ` · ${clockLabel(duration)}`}
        {!isSourceLoading && !sourceError && width > 0 && height > 0 && ` · ${width}×${height}`}
      </p>

      {isSourceLoading && (
        <div className="flow-edit-trim flow-edit-trim-placeholder">
          <p className="flow-edit-trim-placeholder-text">
            Đang tải video lên Google Flow và xử lý thông số để tạo thanh cắt video...
          </p>
        </div>
      )}

      {!isSourceLoading && !sourceError && uploadedMediaId && duration > 0 && (
        <div className="flow-edit-trim" role="group" aria-label="Đoạn video dùng để chỉnh sửa">
          <label className="flow-edit-trim-row">
            <span>Bắt đầu</span>
            <input
              type="range"
              min={0}
              max={Math.max(0, duration - MIN_TRIM_SECONDS)}
              step={TRIM_STEP_SECONDS}
              value={trimStart}
              disabled={generating}
              // Moving the start keeps the chosen length where the video allows.
              onChange={(e) => {
                const start = Number(e.target.value);
                setTrim(start, start + length);
              }}
            />
            <output>{clockLabel(trimStart)}</output>
          </label>
          <label className="flow-edit-trim-row">
            <span>Kết thúc</span>
            <input
              type="range"
              min={MIN_TRIM_SECONDS}
              max={duration}
              step={TRIM_STEP_SECONDS}
              value={trimEnd}
              disabled={generating}
              onChange={(e) => setTrim(trimStart, Number(e.target.value))}
            />
            <output>{clockLabel(trimEnd)}</output>
          </label>
          <p className="flow-edit-trim-summary">
            Dùng {length.toFixed(1)} giây
            {maxInput > 0 && ` · model đọc tối đa ${maxInput} giây`}
            {tooLong && ', video dài hơn nên đã được cắt'}
          </p>
        </div>
      )}
    </div>
  );
}
