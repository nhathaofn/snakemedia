import { useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { FileUp, Pause, Send } from 'lucide-react';
import { BATCH_MAX_REPEAT } from '../../constants/ui';
import { Button, Icon } from '../ui';
import { splitPromptList } from './prompts';
import './BatchForm.css';

export interface BatchFormProps {
  /** Prefix for element IDs, unique on the page. */
  id: string;
  placeholder?: string;
  /** Extra explanation under the prompt box. */
  hint?: ReactNode;
  /** Shows "Số lần mỗi prompt" when set; the value is passed to onSubmit. */
  repeatLabel?: string;
  /** Provider fields shown between the prompt box and the buttons. */
  children?: ReactNode;
  /** Why the batch cannot be added now; undefined when ready. */
  blockedReason?: string;
  /**
   * Adds the prompts to the queue, started (held=false) or paused (held=true).
   * Resolve true when they were added, so the prompt box clears.
   */
  onSubmit: (prompts: string[], repeat: number, held: boolean) => Promise<boolean>;
}

/**
 * The batch form every provider uses: one prompt box with one split rule
 * (splitPromptList), an optional repeat count, a .txt import, and "Thêm & chạy"
 * or "Thêm (tạm giữ)".
 */
export function BatchForm({
  id,
  placeholder,
  hint,
  repeatLabel,
  children,
  blockedReason,
  onSubmit,
}: BatchFormProps) {
  const [text, setText] = useState('');
  const [repeat, setRepeat] = useState(1);
  const [submitting, setSubmitting] = useState(false);
  const [fileError, setFileError] = useState<string | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const prompts = splitPromptList(text);
  const total = prompts.length * (repeatLabel ? repeat : 1);
  const disabled = submitting || total === 0 || !!blockedReason;

  const submit = async (held: boolean) => {
    if (disabled) return;
    setSubmitting(true);
    try {
      if (await onSubmit(prompts, repeatLabel ? repeat : 1, held)) setText('');
    } finally {
      setSubmitting(false);
    }
  };

  const importFile = async (file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.txt')) {
      setFileError('Chỉ hỗ trợ tệp .txt.');
      return;
    }
    try {
      const content = await file.text();
      setText((current) => (current.trim() ? `${current.trimEnd()}\n\n${content}` : content));
      setFileError(null);
    } catch {
      setFileError('Không đọc được tệp. Hãy thử lại.');
    }
  };

  return (
    <form
      className="batch-form"
      onSubmit={(event) => {
        event.preventDefault();
        void submit(false);
      }}
    >
      <label className="batch-field" htmlFor={`${id}-prompts`}>
        <span>
          Prompt — mỗi dòng một tác vụ; prompt nhiều dòng thì cách nhau bằng một dòng trống
        </span>
      </label>
      <textarea
        id={`${id}-prompts`}
        className="batch-input batch-textarea"
        rows={6}
        value={text}
        placeholder={placeholder}
        onChange={(event) => setText(event.target.value)}
      />
      <div className="batch-meta">
        <span>{prompts.length} prompt</span>
        <input
          ref={fileRef}
          type="file"
          accept=".txt,text/plain"
          hidden
          onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            event.currentTarget.value = '';
            void importFile(file);
          }}
        />
        <Button type="button" size="sm" variant="ghost" onClick={() => fileRef.current?.click()}>
          <Icon icon={FileUp} size="sm" />
          <span>Nhập từ tệp .txt</span>
        </Button>
      </div>
      {fileError && (
        <p className="batch-error" role="alert">
          {fileError}
        </p>
      )}
      {hint && <div className="batch-hint">{hint}</div>}

      {repeatLabel && (
        <label className="batch-inline-field">
          <span>{repeatLabel}</span>
          <input
            className="batch-input batch-number"
            type="number"
            min={1}
            max={BATCH_MAX_REPEAT}
            value={repeat}
            onChange={(event) =>
              setRepeat(
                Math.min(
                  BATCH_MAX_REPEAT,
                  Math.max(1, Math.floor(Number(event.target.value) || 1)),
                ),
              )
            }
          />
          <span className="batch-hint">tối đa {BATCH_MAX_REPEAT}</span>
        </label>
      )}

      {children}

      {blockedReason && <p className="batch-hint">{blockedReason}</p>}
      <div className="batch-actions">
        <Button type="submit" variant="primary" isLoading={submitting} disabled={disabled}>
          <Icon icon={Send} />
          <span>{total > 1 ? `Thêm & chạy ${total} tác vụ` : 'Thêm & chạy'}</span>
        </Button>
        <Button
          type="button"
          variant="secondary"
          disabled={disabled}
          onClick={() => void submit(true)}
        >
          <Icon icon={Pause} size="sm" />
          <span>Thêm (tạm giữ)</span>
        </Button>
      </div>
    </form>
  );
}
