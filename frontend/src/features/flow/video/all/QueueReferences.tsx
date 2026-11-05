import { thumbUrl } from '../../../../services';
import { ImagePlus, X } from 'lucide-react';
import { Button, Icon } from '../../../../components/ui';
import { api } from '../../../../services/api';
import { useAppStore } from '../../../../stores';
import { useFlowVideoStore } from '../store/flowVideoStore';

/**
 * Reference images given to the video prompts added next. Files are copied into the
 * app's media folder by the backend; only their URLs live here.
 */
export function QueueReferences({ limit }: { limit?: number }) {
  const references = useFlowVideoStore((s) => s.queueReferences);
  const addReferences = useFlowVideoStore((s) => s.addQueueReferences);
  const removeReference = useFlowVideoStore((s) => s.removeQueueReference);
  const addToast = useAppStore((state) => state.addToast);

  if (limit === undefined) {
    return (
      <p className="flow-video-all-config-empty">
        Chưa có dữ liệu model để xác định ảnh tham chiếu.
      </p>
    );
  }
  if (limit === 0 && references.length === 0) {
    return (
      <p className="flow-video-all-config-empty">Model hiện tại không hỗ trợ ảnh tham chiếu.</p>
    );
  }

  const pick = async () => {
    const response = await api.pickFlowVideoReferences();
    if (!response.success) {
      addToast({
        type: 'error',
        title: 'Không thêm được ảnh tham chiếu',
        message: response.error ?? 'Không rõ lỗi.',
      });
      return;
    }
    const picked = response.data ?? [];
    const room = Math.max(0, limit - references.length);
    addReferences(picked.slice(0, room));
    if (picked.length > room) {
      addToast({
        type: 'warning',
        title: 'Một số ảnh không được thêm',
        message: `Model này nhận tối đa ${limit} ảnh tham chiếu.`,
      });
    }
  };

  return (
    <div className="flow-video-all-queue-references">
      <div className="flow-video-all-queue-reference-grid">
        {references.map((ref) => (
          <figure className="flow-video-all-queue-reference" key={ref.localUrl}>
            <img
              src={thumbUrl(ref.localUrl)}
              alt={ref.fileName}
              title={ref.fileName}
              loading="lazy"
              decoding="async"
            />
            <button
              type="button"
              aria-label={`Bỏ ${ref.fileName}`}
              title="Bỏ ảnh"
              onClick={() => removeReference(ref.localUrl)}
            >
              <Icon icon={X} size="sm" />
            </button>
          </figure>
        ))}
      </div>
      {references.length > limit && (
        <p className="flow-video-all-config-empty" role="alert">
          Đã chọn {references.length}/{limit} ảnh. Vượt quá giới hạn của model này.
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={references.length >= limit}
        onClick={() => void pick()}
      >
        <Icon icon={ImagePlus} size="sm" />
        Thêm ảnh ({references.length}/{limit})
      </Button>
    </div>
  );
}
