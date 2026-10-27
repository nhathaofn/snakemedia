import { useState } from 'react';
import { X } from 'lucide-react';
import { Button, Icon } from '../../../components/ui';
import { useMuseStore } from './museStore';

/**
 * Banners at the top of a Muse workspace: the queue could not be loaded, or
 * the job list was unreadable on start and moved aside (jobs it held are no
 * longer listed, and sent ones may still be running on muse.ai).
 */
export function MuseNotices() {
  const loadError = useMuseStore((s) => s.loadError);
  const journalWarning = useMuseStore((s) => s.options?.journalWarning);
  const [dismissed, setDismissed] = useState(false);
  if (!loadError && (!journalWarning || dismissed)) return null;
  return (
    <div className="muse-notices">
      {loadError && (
        <div role="alert" className="muse-alert muse-alert-danger">
          {loadError}
        </div>
      )}
      {journalWarning && !dismissed && (
        <div role="alert" className="muse-alert muse-alert-warning">
          <span>
            {journalWarning}. Các tác vụ cũ không còn trong danh sách; tác vụ đã gửi có thể vẫn đang
            chạy — hãy kiểm tra trên muse.ai trước khi tạo lại.
          </span>
          <Button
            size="sm"
            variant="ghost"
            aria-label="Ẩn cảnh báo"
            onClick={() => setDismissed(true)}
          >
            <Icon icon={X} />
          </Button>
        </div>
      )}
    </div>
  );
}
