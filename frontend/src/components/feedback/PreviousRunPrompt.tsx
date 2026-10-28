import { Button, Modal } from '../ui';
import { usePreviousRunStore } from '../../stores';
import './PreviousRunPrompt.css';

/**
 * Asked once when the app opens, after every provider reported what its
 * earlier run left: show those results again, or start with clean screens.
 * Either way nothing is deleted, and each screen's "Phiên cũ" button can
 * switch it later.
 */
export function PreviousRunPrompt() {
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
  const setShowPrevious = usePreviousRunStore((s) => s.setShowPrevious);
  const reports = Object.values(usePreviousRunStore((s) => s.reports));
  if (showPrevious !== null || reports.length === 0 || reports.some((r) => r === null)) return null;
  const parts = reports.flatMap((r) => r ?? []);
  if (parts.length === 0) return null;
  return (
    <Modal
      title="Mở lại phiên trước?"
      onClose={() => setShowPrevious(false)}
      footer={
        <div className="previous-run-actions">
          <Button onClick={() => setShowPrevious(false)}>Không</Button>
          <Button variant="primary" onClick={() => setShowPrevious(true)}>
            Có, hiển thị lại
          </Button>
        </div>
      }
    >
      <div className="previous-run-body">
        <p>Lần trước đã tạo {parts.join(', ')}. Hiển thị lại, hay bắt đầu với màn trống?</p>
        <p className="previous-run-hint">
          "Không" chỉ ẩn khỏi màn tạo, không xoá file. Mở lại bất cứ lúc nào bằng nút "Phiên cũ"
          trên màn đó; Thư viện vẫn giữ mọi file.
        </p>
      </div>
    </Modal>
  );
}
