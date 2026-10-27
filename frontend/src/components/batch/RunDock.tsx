import { Pause, Play } from 'lucide-react';
import { Button, Icon } from '../ui';

/**
 * The bar under a batch panel: "Chạy" starts the prompts on hold, "Dừng" holds
 * the ones waiting to run (running ones finish).
 */
export function RunDock({
  heldCount,
  waitingCount,
  onRun,
  onStop,
}: {
  heldCount: number;
  waitingCount: number;
  onRun: () => void;
  onStop: () => void;
}) {
  return (
    <div className="studio-action-dock">
      <Button
        variant="primary"
        size="sm"
        disabled={heldCount === 0}
        title="Chạy các prompt đang chờ."
        onClick={onRun}
      >
        <Icon icon={Play} size="sm" />
        <span>Chạy{heldCount > 0 ? ` ${heldCount} prompt` : ''}</span>
      </Button>
      <Button
        variant="outline"
        size="sm"
        disabled={waitingCount === 0}
        title="Tác vụ đang chạy sẽ hoàn tất; các tác vụ chờ được giữ lại."
        onClick={onStop}
      >
        <Icon icon={Pause} size="sm" />
        <span>Dừng</span>
      </Button>
    </div>
  );
}
