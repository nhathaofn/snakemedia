import { History } from 'lucide-react';
import { Button, Icon } from '../ui';
import { usePreviousRunStore } from '../../stores';

/**
 * Shows or hides the earlier run's finished results on a create screen. The
 * choice is shared: every screen that lists past results follows it.
 */
export function PreviousRunToggle({ count }: { count: number }) {
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
  const setShowPrevious = usePreviousRunStore((s) => s.setShowPrevious);
  if (count === 0) return null;
  return (
    <Button
      size="sm"
      variant={showPrevious ? 'primary' : 'ghost'}
      aria-pressed={!!showPrevious}
      title={showPrevious ? 'Ẩn kết quả của phiên trước' : 'Hiển thị lại kết quả của phiên trước'}
      onClick={() => setShowPrevious(!showPrevious)}
    >
      <Icon icon={History} size="sm" />
      <span>
        {showPrevious ? 'Ẩn phiên cũ' : 'Phiên cũ'} ({count})
      </span>
    </Button>
  );
}
