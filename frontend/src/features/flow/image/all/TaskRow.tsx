import { thumbUrl } from '../../../../services';
import { memo } from 'react';
import { QueuePrompt, QueueProgress } from '../../../../components/batch';
import type { QueueTone } from '../../../../components/batch';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask, GeneratedImageItem } from '../../../../types';

export interface TaskRowActions {
  toggle: (id: string, checked: boolean) => void;
  preview: (image: GeneratedImageItem) => void;
  showReferences: (task: FlowImageTask, number: number) => void;
  cancel: (id: string) => void;
  retry: (id: string) => void;
  edit: (id: string, prompt: string) => void;
  reveal: (path: string) => void;
}

interface TaskRowProps {
  task: FlowImageTask;
  number: number;
  referenceLimit: number;
  selected: boolean;
  actions: TaskRowActions;
}

/** Labels as on Muse's queue. */
function statusLabel(task: FlowImageTask): string {
  switch (task.status) {
    case FlowImageTaskStatus.Queued:
      return task.held ? 'Tạm dừng' : 'Chờ chạy';
    case FlowImageTaskStatus.Running:
      return 'Đang chạy';
    case FlowImageTaskStatus.Succeeded:
      return 'Xong';
    case FlowImageTaskStatus.Cancelled:
      return 'Đã huỷ';
    default:
      return 'Lỗi';
  }
}

function toneOf(task: FlowImageTask): QueueTone {
  if (task.status === FlowImageTaskStatus.Queued) return task.held ? 'held' : 'queued';
  return task.status as QueueTone;
}

/** One task of the image queue; re-renders only when its own task changes. */
export const TaskRow = memo(function TaskRow({
  task,
  number,
  referenceLimit,
  selected,
  actions,
}: TaskRowProps) {
  const running = task.status === FlowImageTaskStatus.Running;
  const ended =
    task.status === FlowImageTaskStatus.Failed || task.status === FlowImageTaskStatus.Cancelled;
  const shownReferences = task.references.slice(0, 2);
  const overflow = Math.max(task.references.length - shownReferences.length, 0);
  const results = task.results.filter((result) => result.localUrl);
  const saved = task.savedPaths?.[0];
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
        editable={!running}
        saveLabel={task.status === FlowImageTaskStatus.Queued ? 'Lưu' : 'Lưu & chạy lại'}
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
              aria-label="Phóng to ảnh kết quả"
              title="Xem ảnh lớn"
              onClick={() => actions.preview(result)}
            >
              <img
                className="queue-result-thumb"
                src={thumbUrl(result.localUrl)}
                alt={result.prompt || task.prompt}
                loading="lazy"
                decoding="async"
              />
            </button>
          ))
        ) : (
          <span className="queue-muted">—</span>
        )}
      </div>

      <QueueProgress
        tone={toneOf(task)}
        label={statusLabel(task)}
        detail={running ? task.step : undefined}
        error={ended || task.status === FlowImageTaskStatus.Succeeded ? task.error : undefined}
        savedPath={task.status === FlowImageTaskStatus.Succeeded ? saved : undefined}
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
