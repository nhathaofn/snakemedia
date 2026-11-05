import { thumbUrl } from '../../../../services';
import { memo } from 'react';
import { Play } from 'lucide-react';
import { Icon } from '../../../../components/ui';
import { QueuePrompt, QueueProgress } from '../../../../components/batch';
import type { QueueTone } from '../../../../components/batch';
import { FlowVideoTaskStatus } from '../../../../types';
import type { FlowVideoTask, GeneratedVideoItem } from '../../../../types';

export interface TaskRowActions {
  toggle: (id: string, checked: boolean) => void;
  preview: (video: GeneratedVideoItem) => void;
  showReferences: (task: FlowVideoTask, number: number) => void;
  cancel: (id: string) => void;
  retry: (id: string) => void;
  edit: (id: string, prompt: string) => void;
  reveal: (path: string) => void;
}

interface TaskRowProps {
  task: FlowVideoTask;
  number: number;
  referenceLimit: number;
  selected: boolean;
  actions: TaskRowActions;
}

/** Labels as on Muse's queue. */
function statusLabel(task: FlowVideoTask): string {
  switch (task.status) {
    case FlowVideoTaskStatus.Queued:
      return task.held ? 'Tạm dừng' : 'Chờ chạy';
    case FlowVideoTaskStatus.Running:
      return 'Đang chạy';
    case FlowVideoTaskStatus.Succeeded:
      return 'Xong';
    case FlowVideoTaskStatus.Cancelled:
      return 'Đã huỷ';
    default:
      return 'Lỗi';
  }
}

function toneOf(task: FlowVideoTask): QueueTone {
  if (task.status === FlowVideoTaskStatus.Queued) return task.held ? 'held' : 'queued';
  return task.status as QueueTone;
}

/** One task of the video queue; re-renders only when its own task changes. */
export const TaskRow = memo(function TaskRow({
  task,
  number,
  referenceLimit,
  selected,
  actions,
}: TaskRowProps) {
  const running = task.status === FlowVideoTaskStatus.Running;
  const ended =
    task.status === FlowVideoTaskStatus.Failed || task.status === FlowVideoTaskStatus.Cancelled;
  const shownReferences = task.references.slice(0, 2);
  const overflow = Math.max(task.references.length - shownReferences.length, 0);
  const results = task.results.filter((result) => result.localUrl);
  const saved = task.savedPaths?.[0];

  const formatInfo = [task.aspectRatio, task.duration ? `${task.duration}s` : '', task.resolution]
    .filter(Boolean)
    .join(' · ');

  return (
    <div className={`queue-table-row${selected ? ' is-selected' : ''}`} role="row">
      <span className="queue-table-index" role="cell">
        {number}
        <input
          type="checkbox"
          aria-label={`Chọn tác vụ ${number}`}
          checked={selected}
          disabled={running}
          onChange={(event) => actions.toggle(task.id, event.currentTarget.checked)}
        />
      </span>

      <QueuePrompt
        prompt={task.prompt}
        editable={!running && !task.requestId}
        saveLabel={task.status === FlowVideoTaskStatus.Queued ? 'Lưu' : 'Lưu & chạy lại'}
        onSave={(prompt) => actions.edit(task.id, prompt)}
      />

      <div className="queue-table-refs" role="cell">
        {task.references.length > 0 ? (
          <>
            {shownReferences.map((reference) => (
              <img
                className="queue-ref-thumb"
                key={reference.localUrl}
                src={thumbUrl(reference.localUrl)}
                alt={reference.fileName}
                title={reference.fileName}
                loading="lazy"
                decoding="async"
              />
            ))}
            {overflow > 0 && (
              <button
                type="button"
                className="queue-ref-overflow"
                aria-label={`Xem ảnh tham chiếu của tác vụ ${number}: ${task.references.length} đã chọn, tối đa ${referenceLimit}`}
                title={`${task.references.length}/${referenceLimit} ảnh đã chọn`}
                onClick={() => actions.showReferences(task, number)}
              >
                +{overflow}
              </button>
            )}
          </>
        ) : (
          <span className="queue-ref-empty">Chưa có</span>
        )}
      </div>

      <div className="queue-table-results" role="cell">
        {results.length > 0 ? (
          results.map((result, index) => (
            <button
              type="button"
              className="queue-result"
              key={`${task.id}-result-${result.id || index}`}
              aria-label="Xem video kết quả"
              title="Xem video lớn"
              onClick={() => actions.preview(result)}
            >
              <video
                className="queue-result-thumb"
                src={result.localUrl}
                preload="metadata"
                muted
              />
              <span className="queue-result-play" aria-hidden="true">
                <Icon icon={Play} size="sm" />
              </span>
            </button>
          ))
        ) : (
          <span className="queue-muted">—</span>
        )}
      </div>

      <QueueProgress
        tone={toneOf(task)}
        label={statusLabel(task)}
        title={formatInfo || undefined}
        detail={running ? task.step : formatInfo || undefined}
        error={ended || task.status === FlowVideoTaskStatus.Succeeded ? task.error : undefined}
        savedPath={task.status === FlowVideoTaskStatus.Succeeded ? saved : undefined}
        onReveal={saved ? () => actions.reveal(saved) : undefined}
        action={
          running
            ? { label: 'Hủy', onClick: () => actions.cancel(task.id) }
            : ended
              ? {
                  label: task.requestId ? 'Kiểm tra lại' : 'Chạy lại',
                  title: task.requestId
                    ? 'Kiểm tra lại job Flow đã nhận, không gửi yêu cầu mới.'
                    : 'Gửi lại tác vụ này.',
                  onClick: () => actions.retry(task.id),
                }
              : undefined
        }
      />
    </div>
  );
});
