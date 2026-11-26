import { ImagePlus, Images, RotateCcw, Trash2 } from 'lucide-react';
import { Button, Icon, IconAction, confirmAction } from '../../../../components/ui';

interface QueueActionsProps {
  selectedCount: number;
  /** Selected tasks that ended (failed or cancelled). */
  retryableCount: number;
  failedCount: number;
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
 * References go to selected tasks or eligible tasks in the current filter. Every
 * deletion asks first and keeps saved files.
 */
export function QueueActions({
  selectedCount,
  retryableCount,
  failedCount,
  removableCount,
  canAddReferences,
  onPickReferences,
  onOpenBank,
  onRetrySelected,
  onRemoveSelected,
  onRetryFailed,
  onRemoveAll,
}: QueueActionsProps) {
  const confirmThen = (title: string, message: string, action: () => void) => () => {
    void (async () => {
      if (await confirmAction({ title, message, confirmLabel: 'Xóa', danger: true })) {
        action();
      }
    })();
  };
  return (
    <>
      <IconAction
        icon={ImagePlus}
        label="Thêm ảnh tham chiếu cho tác vụ đã chọn hoặc trong bộ lọc"
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
              'Xóa tác vụ đã chọn?',
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
            label="Chạy lại các tác vụ lỗi"
            disabled={failedCount === 0}
            onClick={onRetryFailed}
          />
          <IconAction
            icon={Trash2}
            label="Xóa toàn bộ hàng đợi"
            disabled={removableCount === 0}
            onClick={confirmThen(
              'Xóa toàn bộ hàng đợi?',
              'Xóa toàn bộ tác vụ khỏi hàng đợi? Tác vụ đang chạy được giữ lại; ảnh đã lưu vẫn còn trong thư mục.',
              onRemoveAll,
            )}
          />
        </>
      )}
    </>
  );
}
