import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react';
import { RefreshCw, Sparkles, Video, X } from 'lucide-react';
import { cssAspectRatio } from './format';
import { conversationItemKey } from './studioStore';
import type { ConversationAttachment, ConversationTurn, StudioResult } from './studioStore';

/** Shared prompt/result conversation used by the Image, Video and Edit studios. */
export function ConversationGallery<TItem extends StudioResult>({
  turns,
  items,
  emptyTitle,
  emptyText,
  renderItem,
}: {
  turns: ConversationTurn[];
  items: TItem[];
  emptyTitle: string;
  emptyText: string;
  renderItem: (item: TItem, turn: ConversationTurn) => ReactNode;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [preview, setPreview] = useState<ConversationAttachment | null>(null);
  const itemByKey = new Map(items.map((item) => [conversationItemKey(item), item]));
  const latest = turns.at(-1);
  const scrollKey = latest ? `${latest.id}:${latest.resultKeys.length}:${latest.pendingCount}` : '';

  useEffect(() => {
    const element = scrollRef.current;
    if (element) element.scrollTo({ top: element.scrollHeight, behavior: 'smooth' });
  }, [scrollKey]);

  useEffect(() => {
    if (!preview) return;
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPreview(null);
    };
    window.addEventListener('keydown', closeOnEscape);
    return () => window.removeEventListener('keydown', closeOnEscape);
  }, [preview]);

  return (
    <div className="flow-workspace-gallery flow-conversation-gallery" ref={scrollRef}>
      {turns.length === 0 ? (
        <div className="flow-gallery-empty">
          <Sparkles size={40} className="flow-empty-icon" />
          <h3>{emptyTitle}</h3>
          <p>{emptyText}</p>
        </div>
      ) : (
        <div className="flow-chat-thread">
          {turns.map((turn) => {
            const results = turn.resultKeys
              .map((key) => itemByKey.get(key))
              .filter((item): item is TItem => item !== undefined);
            return (
              <article className="flow-chat-turn" key={turn.id}>
                <div className="flow-chat-row flow-chat-user-row">
                  <div className="flow-chat-user-message">
                    <span className="flow-chat-role">Bạn</span>
                    <p className="flow-chat-prompt">{turn.prompt}</p>
                    {turn.attachments.length > 0 && (
                      <div className="flow-chat-attachments">
                        {turn.attachments.map((attachment) => (
                          <button
                            type="button"
                            key={attachment.id}
                            className="flow-chat-attachment"
                            title={`Mở ${attachment.name}`}
                            onClick={() => setPreview(attachment)}
                          >
                            <span className="flow-chat-attachment-preview">
                              {attachment.kind === 'image' ? (
                                <img src={attachment.url} alt={attachment.name} />
                              ) : (
                                <>
                                  <video src={attachment.url} muted preload="metadata" />
                                  <Video size={16} className="flow-chat-attachment-video-icon" />
                                </>
                              )}
                            </span>
                            <span className="flow-chat-attachment-name">{attachment.name}</span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {(results.length > 0 || turn.pendingCount > 0 || turn.error) && (
                  <div className="flow-chat-row flow-chat-ai-row">
                    <span className="flow-chat-ai-icon" aria-label="Google Flow">
                      <Sparkles size={16} />
                    </span>
                    <div className="flow-chat-ai-message">
                      <span className="flow-chat-role">Flow</span>
                      <div className="flow-chat-results">
                        {results.map((item) => (
                          <div className="flow-chat-result" key={conversationItemKey(item)}>
                            {renderItem(item, turn)}
                          </div>
                        ))}
                        {Array.from({ length: turn.pendingCount }).map((_, index) => (
                          <div
                            key={`pending-${turn.id}-${index}`}
                            className="flow-media-card flow-chat-pending skeleton"
                            style={
                              turn.pendingRatio
                                ? { aspectRatio: cssAspectRatio(turn.pendingRatio) }
                                : undefined
                            }
                          >
                            <RefreshCw size={20} className="skeleton-spinner" />
                            <span>
                              {turn.pendingLabel} {index + 1}/{turn.pendingCount}…
                            </span>
                          </div>
                        ))}
                        {turn.error && (
                          <p className="flow-chat-error" role="alert">
                            {turn.error}
                          </p>
                        )}
                      </div>
                    </div>
                  </div>
                )}
              </article>
            );
          })}
        </div>
      )}
      {preview &&
        createPortal(
          <div className="flow-chat-preview-overlay" onClick={() => setPreview(null)}>
            <div
              className="flow-chat-preview-dialog"
              role="dialog"
              aria-modal="true"
              aria-label={preview.name}
              onClick={(event) => event.stopPropagation()}
            >
              <button
                type="button"
                className="flow-chat-preview-close"
                aria-label="Đóng xem trước"
                onClick={() => setPreview(null)}
              >
                <X size={16} />
              </button>
              {preview.kind === 'image' ? (
                <img src={preview.url} alt={preview.name} />
              ) : (
                <video src={preview.url} controls preload="metadata" />
              )}
              <span className="flow-chat-preview-name">{preview.name}</span>
            </div>
          </div>,
          document.body,
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
          title={
            blockedReason ?? (generating ? 'Gửi prompt mới trong khi tác vụ đang chạy.' : undefined)
          }
          onClick={onGenerate}
        >
          <Sparkles size={18} />
          <span>{generating ? 'Tạo tiếp' : 'Tạo'}</span>
        </button>
      </div>
      {blockedReason && <p className="flow-prompt-hint">{blockedReason}</p>}
    </div>
  );
}
