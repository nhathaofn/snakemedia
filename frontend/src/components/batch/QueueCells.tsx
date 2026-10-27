import { useState } from 'react';
import { FolderOpen } from 'lucide-react';
import { Button, Icon } from '../ui';

/**
 * A row's prompt. A double click on an editable prompt opens a box that fills
 * the cell: Enter saves, Ctrl+Enter breaks the line, Esc closes it.
 */
export function QueuePrompt({
  prompt,
  editable,
  saveLabel,
  onSave,
}: {
  prompt: string;
  /** False while the task runs. */
  editable: boolean;
  /** "Lưu" for a task still waiting, "Lưu & chạy lại" for one that ended. */
  saveLabel: string;
  onSave: (prompt: string) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const save = () => {
    if (draft === null || !draft.trim()) return;
    if (draft.trim() !== prompt.trim()) onSave(draft);
    setDraft(null);
  };

  if (draft === null) {
    return (
      <div className="queue-table-prompt" role="cell">
        <span
          title={editable ? `${prompt}\n\nNhấp đúp để sửa` : prompt}
          onDoubleClick={editable ? () => setDraft(prompt) : undefined}
        >
          {prompt}
        </span>
      </div>
    );
  }
  return (
    <div className="queue-table-prompt" role="cell">
      <form
        className="queue-edit"
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
      >
        <textarea
          className="batch-input queue-edit-input"
          rows={3}
          value={draft}
          autoFocus
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Escape') setDraft(null);
            if (e.key !== 'Enter' || e.nativeEvent.isComposing || e.shiftKey) return;
            e.preventDefault();
            if (e.ctrlKey || e.metaKey) {
              const box = e.currentTarget;
              const at = box.selectionStart;
              setDraft(`${draft.slice(0, at)}\n${draft.slice(box.selectionEnd)}`);
              requestAnimationFrame(() => box.setSelectionRange(at + 1, at + 1));
            } else save();
          }}
        />
        <div className="queue-edit-actions">
          <Button size="sm" variant="primary" type="submit">
            {saveLabel}
          </Button>
          <Button size="sm" variant="ghost" type="button" onClick={() => setDraft(null)}>
            Huỷ
          </Button>
        </div>
      </form>
    </div>
  );
}

/** How a row's task stands, for its colour: the backend's status or "held". */
export type QueueTone = 'queued' | 'held' | 'running' | 'succeeded' | 'failed' | 'cancelled';

/**
 * A row's progress, kept short: the status, one line of detail (the step
 * while running, the error when it failed, a note about the result), and one
 * action: Hủy while running, Chạy lại when it ended without a result, and the
 * folder of the saved file once it succeeded.
 */
export function QueueProgress({
  tone,
  label,
  title,
  detail,
  error,
  warning,
  savedPath,
  onReveal,
  action,
}: {
  tone: QueueTone;
  label: string;
  /** Extra information on hover (the account, say). */
  title?: string;
  detail?: string;
  error?: string;
  warning?: string;
  /** Where the result was saved; shown as its folder, opened on click. */
  savedPath?: string;
  onReveal?: () => void;
  action?: { label: string; title?: string; onClick: () => void };
}) {
  const folder = savedPath ? folderOf(savedPath) : '';
  return (
    <div className="queue-table-progress" role="cell">
      <span className={`queue-status is-${tone}`} title={title}>
        {tone === 'running' && <span className="queue-spinner" aria-hidden="true" />}
        {label}
      </span>
      {detail && (
        <span className="queue-muted queue-line" title={detail}>
          {detail}
        </span>
      )}
      {error && (
        <span className="queue-error" title={error}>
          {error}
        </span>
      )}
      {warning && (
        <span className="queue-warning queue-line" title={warning}>
          {warning}
        </span>
      )}
      {folder && onReveal && (
        <button
          type="button"
          className="queue-folder"
          title={`Mở thư mục: ${savedPath}`}
          onClick={onReveal}
        >
          <Icon icon={FolderOpen} size="sm" />
          <span>{folder}</span>
        </button>
      )}
      {action && (
        <button
          type="button"
          className="queue-row-action"
          title={action.title}
          onClick={action.onClick}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

/** The name of the folder that holds path (Windows or POSIX separators). */
function folderOf(path: string): string {
  const parts = path.split(/[\\/]+/).filter(Boolean);
  return parts.length > 1 ? parts[parts.length - 2] : path;
}
