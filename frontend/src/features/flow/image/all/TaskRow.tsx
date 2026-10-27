import { memo } from 'react';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask, GeneratedImageItem } from '../../../../types';

export interface TaskRowActions {
  toggle: (id: string, checked: boolean) => void;
  preview: (image: GeneratedImageItem) => void;
  showReferences: (task: FlowImageTask, number: number) => void;
  cancel: (id: string) => void;
  retry: (id: string) => void;
}

interface TaskRowProps {
  task: FlowImageTask;
  number: number;
  batchNumber: number;
  modelName: string;
  referenceLimit: number;
  selected: boolean;
  actions: TaskRowActions;
}

function statusLabel(task: FlowImageTask): string {
  switch (task.status) {
    case FlowImageTaskStatus.Queued:
      return task.held ? 'Chờ bắt đầu' : 'Chờ xử lý';
    case FlowImageTaskStatus.Running:
      return 'Đang xử lý';
    case FlowImageTaskStatus.Succeeded:
      return 'Hoàn tất';
    case FlowImageTaskStatus.Cancelled:
      return 'Đã hủy';
    default:
      return 'Thất bại';
  }
}

/** One task of the image queue; re-renders only when its own task changes. */
export const TaskRow = memo(function TaskRow({
  task,
  number,
  batchNumber,
  modelName,
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
  return (
    <div className={`flow-image-all-card${selected ? ' is-selected' : ''}`} role="row">
      <span className="flow-image-all-index" role="cell">
        {number}
        <input
          type="checkbox"
          aria-label={`Chọn tác vụ ${number}`}
          checked={selected}
          disabled={running}
          onChange={(event) => actions.toggle(task.id, event.currentTarget.checked)}
        />
      </span>

      <div className="flow-image-all-task-meta" role="cell">
        <strong>Lô {batchNumber}</strong>
        <span>{modelName}</span>
        <span>{task.aspectRatio || 'Chưa có'}</span>
        <span>{task.count} ảnh / prompt</span>
      </div>

      <span className="flow-image-all-prompt" role="cell" title={task.prompt}>
        {task.prompt}
      </span>

      <div className="flow-image-all-references" role="cell">
        {task.references.length > 0 ? (
          <>
            {shownReferences.map((reference) => (
              <img
                className="flow-image-all-reference-thumb"
                key={reference.localUrl}
                src={reference.localUrl}
                alt={reference.fileName}
                title={reference.fileName}
                loading="lazy"
              />
            ))}
            {overflow > 0 && (
              <button
                type="button"
                className="flow-image-all-reference-overflow"
                aria-label={`Xem ảnh tham chiếu của tác vụ ${number}: ${task.references.length} đã chọn, tối đa ${referenceLimit}`}
                title={`${task.references.length}/${referenceLimit} ảnh đã chọn`}
                onClick={() => actions.showReferences(task, number)}
              >
                +{overflow}
              </button>
            )}
          </>
        ) : (
          <span className="flow-image-all-reference-empty">Chưa có</span>
        )}
      </div>

      <div className="flow-image-all-results" role="cell">
        {results.length > 0 ? (
          results.map((result, index) => (
            <button
              type="button"
              className="flow-image-all-preview-trigger"
              key={`${task.id}-result-${result.id || index}`}
              aria-label="Phóng to ảnh kết quả"
              title="Xem ảnh lớn"
              onClick={() => actions.preview(result)}
            >
              <img
                className="flow-image-all-result-thumb"
                src={result.localUrl}
                alt={result.prompt || task.prompt}
                loading="lazy"
              />
            </button>
          ))
        ) : (
          <span className="flow-image-all-empty-value">—</span>
        )}
      </div>

      <div className="flow-image-all-progress" role="cell">
        <span className={`flow-image-all-status is-${task.status}`}>
          {running && <span className="flow-image-all-spinner" aria-hidden="true" />}
          {statusLabel(task)}
        </span>
        {task.step && (
          <span className="flow-image-all-empty-value" title={task.step}>
            {task.step}
          </span>
        )}
        {task.error && (
          <span className="flow-image-all-task-error" title={task.error}>
            {task.error}
          </span>
        )}
        {task.status === FlowImageTaskStatus.Succeeded && task.savedFiles.length > 0 && (
          <span className="flow-image-all-empty-value">Đã lưu {task.savedFiles.length} ảnh</span>
        )}
        {(task.status === FlowImageTaskStatus.Queued || running) && (
          <button
            type="button"
            className="flow-image-all-row-action"
            onClick={() => actions.cancel(task.id)}
          >
            Hủy
          </button>
        )}
        {ended && (
          <button
            type="button"
            className="flow-image-all-row-action"
            title={
              task.requestId
                ? 'Kiểm tra lại job Flow đã nhận, không gửi yêu cầu mới.'
                : 'Gửi lại tác vụ này.'
            }
            onClick={() => actions.retry(task.id)}
          >
            {task.requestId ? 'Kiểm tra lại' : 'Chạy lại'}
          </button>
        )}
      </div>
    </div>
  );
});
