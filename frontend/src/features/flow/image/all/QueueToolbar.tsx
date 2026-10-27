import { ImagePlus, Images } from 'lucide-react';
import { Button, Icon } from '../../../../components/ui';

interface QueueToolbarProps {
  selectedCount: number;
  retryableCount: number;
  finishedCount: number;
  removableCount: number;
  canAddReferences: boolean;
  onPickReferences: () => void;
  onOpenBank: () => void;
  onRemoveSelected: () => void;
  onClearFinished: () => void;
  onRetrySelected: () => void;
  onRemoveAll: () => void;
}

/** Actions on the image queue; every deletion asks first and keeps saved files. */
export function QueueToolbar({
  selectedCount,
  retryableCount,
  finishedCount,
  removableCount,
  canAddReferences,
  onPickReferences,
  onOpenBank,
  onRemoveSelected,
  onClearFinished,
  onRetrySelected,
  onRemoveAll,
}: QueueToolbarProps) {
  const confirmThen = (message: string, action: () => void) => () => {
    if (window.confirm(message)) action();
  };
  return (
    <div className="flow-image-all-toolbar" aria-label="Thao tác hàng đợi">
      <span className="flow-image-all-selection-count">Đã chọn {selectedCount}</span>
      <div className="flow-image-all-toolbar-actions">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={!canAddReferences}
          onClick={onPickReferences}
        >
          <Icon icon={ImagePlus} size="sm" />
          Thêm ảnh
        </Button>
        <Button type="button" variant="outline" size="sm" onClick={onOpenBank}>
          <Icon icon={Images} size="sm" />
          Kho ảnh
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={selectedCount === 0}
          onClick={confirmThen(
            `Xóa ${selectedCount} tác vụ đã chọn khỏi hàng đợi? Tác vụ đang chạy được giữ lại; ảnh đã lưu vẫn còn trong thư mục.`,
            onRemoveSelected,
          )}
        >
          Xóa đã chọn
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={finishedCount === 0}
          onClick={confirmThen(
            `Xóa ${finishedCount} tác vụ đã xong khỏi hàng đợi? Ảnh đã lưu vẫn còn trong thư mục.`,
            onClearFinished,
          )}
        >
          Xóa đã xong
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={retryableCount === 0}
          title="Tác vụ Flow đã nhận chỉ được kiểm tra lại, không gửi yêu cầu mới."
          onClick={onRetrySelected}
        >
          Chạy lại lỗi
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={removableCount === 0}
          onClick={confirmThen(
            'Xóa tất cả tác vụ không chạy khỏi hàng đợi? Ảnh đã lưu vẫn còn trong thư mục.',
            onRemoveAll,
          )}
        >
          Xóa queue
        </Button>
      </div>
    </div>
  );
}
