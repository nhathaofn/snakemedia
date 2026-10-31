import type { ToolProgress } from '../../types';
import './ToolProgressBar.css';

/** A download's progress; indeterminate until the size is known. */
export function ToolProgressBar({ progress }: { progress: ToolProgress }) {
  const percent = progress.total > 0 ? Math.min(100, (progress.done / progress.total) * 100) : null;
  return (
    <div
      className={`tool-progress ${percent === null ? 'tool-progress-indeterminate' : ''}`}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={percent === null ? undefined : Math.floor(percent)}
    >
      <div
        className="tool-progress-fill"
        style={percent === null ? undefined : { width: `${percent}%` }}
      />
    </div>
  );
}
