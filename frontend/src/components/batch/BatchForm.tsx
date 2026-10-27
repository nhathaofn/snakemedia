import { useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { FileUp } from 'lucide-react';
import { Button, Icon } from '../ui';
import {
  DEFAULT_PROMPT_MODE,
  PROMPT_MODES,
  PROMPT_MODE_HINTS,
  PROMPT_MODE_LABELS,
  isPromptMode,
  newPrompts,
  splitPromptList,
} from './prompts';
import type { PromptMode } from './prompts';
import './BatchForm.css';

const PROMPT_MODE_KEY = 'batch:promptMode';

function loadPromptMode(): PromptMode {
  try {
    const stored = localStorage.getItem(PROMPT_MODE_KEY);
    if (isPromptMode(stored)) return stored;
  } catch {
    // Storage can be unavailable; fall back to the default.
  }
  return DEFAULT_PROMPT_MODE;
}

function savePromptMode(mode: PromptMode) {
  try {
    localStorage.setItem(PROMPT_MODE_KEY, mode);
  } catch {
    // The choice just won't be remembered.
  }
}

export interface BatchFormProps {
  /** Prefix for element IDs, unique on the page. */
  id: string;
  /** Visible lines of the prompt box. */
  rows?: number;
  placeholder?: string;
  /** Extra explanation under the prompt box. */
  hint?: ReactNode;
  /** Provider fields shown under the prompt box. */
  children?: ReactNode;
  /** Why prompts cannot be added now; undefined when ready. */
  blockedReason?: string;
  /** Fixed split mode; hides the "Cách hiểu prompt" picker. */
  promptMode?: PromptMode;
  /** Prompts already waiting in the queue: the same text is not added twice. */
  pending: readonly string[];
  /**
   * Adds the prompts to the queue on hold (the screen's "Chạy" starts them).
   * Resolve true when they were added, so the prompt box clears.
   */
  onAdd: (prompts: string[]) => Promise<boolean>;
}

/**
 * The batch form every provider uses: one prompt box with one split rule
 * (splitPromptList) in a mode the user picks and the browser remembers (or a
 * fixed promptMode). Enter adds the box's prompts to the queue on hold,
 * Ctrl+Enter (or Shift+Enter) breaks the line, and a .txt import adds its
 * prompts at once. Prompts already waiting are skipped (newPrompts).
 */
export function BatchForm({
  id,
  rows = 6,
  placeholder,
  hint,
  children,
  blockedReason,
  promptMode,
  pending,
  onAdd,
}: BatchFormProps) {
  const [text, setText] = useState('');
  const [adding, setAdding] = useState(false);
  const [note, setNote] = useState<string | null>(null);
  const [fileError, setFileError] = useState<string | null>(null);
  const [pickedMode, setMode] = useState<PromptMode>(loadPromptMode);
  const mode = promptMode ?? pickedMode;
  const fileRef = useRef<HTMLInputElement>(null);
  const boxRef = useRef<HTMLTextAreaElement>(null);
  const prompts = splitPromptList(text, mode);

  // Adds list (from the box when fromBox) once; a second Enter while the first
  // is being added does nothing.
  const add = async (list: string[], fromBox: boolean) => {
    if (adding || blockedReason || list.length === 0) return;
    const fresh = newPrompts(list, pending);
    const skipped = list.length - fresh.length;
    if (fresh.length === 0) {
      setNote('Các prompt này đã có trong hàng chờ.');
      if (fromBox) setText('');
      return;
    }
    setAdding(true);
    try {
      if (await onAdd(fresh)) {
        if (fromBox) setText('');
        setNote(skipped > 0 ? `Đã bỏ qua ${skipped} prompt trùng.` : null);
      }
    } finally {
      setAdding(false);
    }
  };

  // Ctrl+Enter puts a line break where the caret is (the browser adds none).
  const breakLine = () => {
    const box = boxRef.current;
    if (!box) return;
    const { selectionStart: from, selectionEnd: to } = box;
    setText(`${text.slice(0, from)}\n${text.slice(to)}`);
    requestAnimationFrame(() => box.setSelectionRange(from + 1, from + 1));
  };

  const importFile = async (file: File | undefined) => {
    if (!file) return;
    if (!file.name.toLowerCase().endsWith('.txt')) {
      setFileError('Chỉ hỗ trợ tệp .txt.');
      return;
    }
    try {
      const content = await file.text();
      setFileError(null);
      await add(splitPromptList(content, mode), false);
    } catch {
      setFileError('Không đọc được tệp. Hãy thử lại.');
    }
  };

  return (
    <div className="batch-form">
      {!promptMode && (
        <label className="batch-inline-field" htmlFor={`${id}-mode`}>
          <span>Cách hiểu prompt</span>
          <select
            id={`${id}-mode`}
            className="batch-input batch-select"
            value={mode}
            onChange={(event) => {
              const next = event.target.value;
              if (!isPromptMode(next)) return;
              setMode(next);
              savePromptMode(next);
            }}
          >
            {PROMPT_MODES.map((value) => (
              <option key={value} value={value}>
                {PROMPT_MODE_LABELS[value]}
              </option>
            ))}
          </select>
          <span className="batch-hint">{PROMPT_MODE_HINTS[mode]}</span>
        </label>
      )}
      <div className="batch-group">
        <div className="batch-meta">
          <label className="batch-field" htmlFor={`${id}-prompts`}>
            <span>Prompt</span>
            <span className="batch-count">{prompts.length} prompt</span>
          </label>
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
          <Button
            type="button"
            size="sm"
            variant="ghost"
            disabled={adding || !!blockedReason}
            title="Thêm các prompt trong tệp vào hàng chờ"
            onClick={() => fileRef.current?.click()}
          >
            <Icon icon={FileUp} size="sm" />
            <span>Nhập từ tệp .txt</span>
          </Button>
        </div>
        <textarea
          ref={boxRef}
          id={`${id}-prompts`}
          className="batch-input batch-textarea"
          rows={rows}
          value={text}
          placeholder={placeholder}
          onChange={(event) => setText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key !== 'Enter' || event.nativeEvent.isComposing || event.shiftKey) return;
            event.preventDefault();
            if (event.ctrlKey || event.metaKey) breakLine();
            else void add(prompts, true);
          }}
        />
        <span className="batch-hint">Enter: thêm vào hàng chờ · Ctrl+Enter: xuống dòng</span>
        {note && <span className="batch-hint">{note}</span>}
        {fileError && (
          <p className="batch-error" role="alert">
            {fileError}
          </p>
        )}
        {hint && <div className="batch-hint">{hint}</div>}
      </div>

      {children}

      {blockedReason && <p className="batch-hint">{blockedReason}</p>}
    </div>
  );
}
