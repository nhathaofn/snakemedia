import { ImagePlus, Images, RotateCcw, Trash2 } from 'lucide-react';
import { Button, Icon, IconAction } from '../../../../components/ui';

interface QueueActionsProps {
  selectedCount: number;
  /** Selected tasks that ended (failed or cancelled). */
  retryableCount: number;
  failedCount: number;
  /** Whether a task is running or waiting to run: failed ones are retried only after. */
  busy: boolean;
  removableCount: number;
  canAddReferences: boolean;
  onPickReferences: () => void;
  onOpenBank: () => void;
  onRetrySelected: () => void;
  onRemoveSelected: () => void;
  onRetryFailed: () => void;
  onRemoveAll: () => void;
}

/**
 * The image queue's actions, in the bar above the table like Muse's queue.
 * References go to the selected tasks, or to every task not sent yet. Every
 * deletion asks first and keeps saved files.
 */
export function QueueActions({
  selectedCount,
  retryableCount,
  failedCount,
  busy,
  removableCount,
  canAddReferences,
  onPickReferences,
  onOpenBank,
  onRetrySelected,
  onRemoveSelected,
  onRetryFailed,
  onRemoveAll,
}: QueueActionsProps) {
  const confirmThen = (message: string, action: () => void) => () => {
    if (window.confirm(message)) action();
  };
  return (
    <>
      <IconAction
        icon={ImagePlus}
        label="Thêm ảnh tham chiếu cho tác vụ đã chọn hoặc chưa gửi"
        disabled={!canAddReferences}
        onClick={onPickReferences}
      />
      <IconAction icon={Images} label="Kho ảnh" onClick={onOpenBank} />
      {selectedCount > 0 ? (
        <>
          {retryableCount > 0 && (
            <Button size="sm" variant="primary" onClick={onRetrySelected}>
              <Icon icon={RotateCcw} size="sm" />
              <span>Chạy lại ({retryableCount})</span>
            </Button>
          )}
          <Button
            size="sm"
            variant="danger"
            onClick={confirmThen(
              `Xóa ${selectedCount} tác vụ đã chọn khỏi hàng đợi? Ảnh đã lưu vẫn còn trong thư mục.`,
              onRemoveSelected,
            )}
          >
            <Icon icon={Trash2} size="sm" />
            <span>Xóa ({selectedCount})</span>
          </Button>
        </>
      ) : (
        <>
          <IconAction
            icon={RotateCcw}
            label={
              busy ? 'Chạy lại các tác vụ lỗi (khi hàng đợi chạy xong)' : 'Chạy lại các tác vụ lỗi'
            }
            disabled={busy || failedCount === 0}
            onClick={onRetryFailed}
          />
          <IconAction
            icon={Trash2}
            label="Xóa toàn bộ hàng đợi"
            disabled={removableCount === 0}
            onClick={confirmThen(
              'Xóa toàn bộ tác vụ khỏi hàng đợi? Tác vụ đang chạy được giữ lại; ảnh đã lưu vẫn còn trong thư mục.',
              onRemoveAll,
            )}
          />
        </>
      )}
    </>
  );
}
