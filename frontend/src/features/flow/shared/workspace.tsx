import { useRef } from 'react';
import type { KeyboardEvent, ReactNode } from 'react';
import { RefreshCw, Sparkles } from 'lucide-react';
import { cssAspectRatio } from './format';

/** Gallery area with the empty state, loading placeholders and the generated items. */
export function MediaGallery({
  isEmpty,
  emptyTitle,
  emptyText,
  pending,
  pendingRatio,
  pendingLabel,
  gridClassName,
  children,
}: {
  isEmpty: boolean;
  emptyTitle: string;
  emptyText: string;
  /** Number of outputs being generated, shown as placeholders before the results. */
  pending: number;
  pendingRatio: string;
  pendingLabel: string;
  gridClassName?: string;
  children: ReactNode;
}) {
  return (
    <div className="flow-workspace-gallery">
      {isEmpty && pending === 0 ? (
        <div className="flow-gallery-empty">
          <Sparkles size={40} className="flow-empty-icon" />
          <h3>{emptyTitle}</h3>
          <p>{emptyText}</p>
        </div>
      ) : (
        <div className={`flow-media-grid ${gridClassName ?? ''}`}>
          {Array.from({ length: pending }).map((_, idx) => (
            <div
              key={`skeleton-${idx}`}
              className="flow-media-card skeleton"
              style={pendingRatio ? { aspectRatio: cssAspectRatio(pendingRatio) } : undefined}
            >
              <div className="skeleton-content">
                <RefreshCw size={20} className="skeleton-spinner" />
                <span>
                  {pendingLabel} {idx + 1}/{pending}…
                </span>
              </div>
            </div>
          ))}
          {children}
        </div>
      )}
    </div>
  );
}

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
  const disabled = generating || !!blockedReason || !prompt.trim();

  const insertNewLine = (el: HTMLTextAreaElement) => {
    const { selectionStart, selectionEnd } = el;
    const next = `${prompt.slice(0, selectionStart)}\n${prompt.slice(selectionEnd)}`;
    onPromptChange(next);
    requestAnimationFrame(() => {
      el.selectionStart = el.selectionEnd = selectionStart + 1;
    });
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Enter' || e.nativeEvent.isComposing || e.shiftKey) return;
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) insertNewLine(e.currentTarget);
    else if (!disabled) onGenerate();
  };

  return (
    <div className="flow-prompt-bar">
      <div className="flow-prompt-input-row">
        <textarea
          ref={textareaRef}
          className="flow-prompt-textarea"
          placeholder={`${placeholder} · Enter để tạo, Ctrl+Enter xuống dòng`}
          value={prompt}
          onChange={(e) => onPromptChange(e.target.value)}
          onKeyDown={onKeyDown}
          rows={2}
        />
        <button
          type="button"
          className="flow-generate-btn"
          disabled={disabled}
          title={blockedReason}
          onClick={onGenerate}
        >
          {generating ? (
            <>
              <RefreshCw size={18} className="spin" />
              <span>Đang tạo...</span>
            </>
          ) : (
            <>
              <Sparkles size={18} />
              <span>Tạo</span>
            </>
          )}
        </button>
      </div>
      {blockedReason && <p className="flow-prompt-hint">{blockedReason}</p>}
    </div>
  );
}
