import { useRef } from 'react';
import type { KeyboardEvent as ReactKeyboardEvent } from 'react';
import { Sparkles } from 'lucide-react';
import { Icon } from '../ui';
import './Studio.css';

/**
 * Pinned prompt bar. Enter generates; Shift+Enter or Ctrl+Enter starts a new line.
 * Typing with an IME (Vietnamese Telex/VNI) never submits mid-composition.
 */
export function PromptBar({
  prompt,
  onPromptChange,
  onGenerate,
  placeholder,
  generating,
  blockedReason,
}: {
  prompt: string;
  onPromptChange: (prompt: string) => void;
  onGenerate: () => void;
  placeholder: string;
  generating: boolean;
  /** Why Generate is unavailable besides an empty prompt; undefined when ready. */
  blockedReason?: string;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const disabled = !!blockedReason || !prompt.trim();

  const insertNewLine = (el: HTMLTextAreaElement) => {
    const { selectionStart, selectionEnd } = el;
    const next = `${prompt.slice(0, selectionStart)}\n${prompt.slice(selectionEnd)}`;
    onPromptChange(next);
    requestAnimationFrame(() => {
      el.selectionStart = el.selectionEnd = selectionStart + 1;
    });
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter' || e.nativeEvent.isComposing || e.shiftKey) return;
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) insertNewLine(e.currentTarget);
    else if (!disabled) onGenerate();
  };

  return (
    <div className="studio-prompt-bar">
      <div className="studio-prompt-input-row">
        <textarea
          ref={textareaRef}
          className="studio-prompt-textarea"
          placeholder={`${placeholder} · Enter để tạo, Ctrl+Enter xuống dòng`}
          value={prompt}
          onChange={(e) => onPromptChange(e.target.value)}
          onKeyDown={onKeyDown}
          rows={2}
        />
        <button
          type="button"
          className="studio-generate-btn"
          disabled={disabled}
          title={
            blockedReason ?? (generating ? 'Gửi prompt mới trong khi tác vụ đang chạy.' : undefined)
          }
          onClick={onGenerate}
        >
          <Icon icon={Sparkles} size="lg" />
          <span>{generating ? 'Tạo tiếp' : 'Tạo'}</span>
        </button>
      </div>
      {blockedReason && <p className="studio-prompt-hint">{blockedReason}</p>}
    </div>
  );
}
