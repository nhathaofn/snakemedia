import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { RefreshCw, Sparkles, Square, Video } from 'lucide-react';
import { Icon, Modal } from '../../../components/ui';
import { cssAspectRatio } from './format';
import { conversationItemKey } from './studioStore';
import type { ConversationAttachment, ConversationTurn, StudioResult } from './studioStore';
import './FlowStudio.css';

/** How close to the end (px) still counts as "at the bottom" of the conversation. */
const BOTTOM_SLACK_PX = 48;

/** Shared prompt/result conversation used by the Image, Video and Edit studios. */
export function ConversationGallery<TItem extends StudioResult>({
  turns,
  items,
  emptyTitle,
  emptyText,
  renderItem,
  onStopTurn,
}: {
  turns: ConversationTurn[];
  items: TItem[];
  emptyTitle: string;
  emptyText: string;
  renderItem: (item: TItem, turn: ConversationTurn) => ReactNode;
  /** Stops waiting on a running turn (its ID is the request ID). */
  onStopTurn?: (turnId: string) => void;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const [preview, setPreview] = useState<ConversationAttachment | null>(null);
  const itemByKey = new Map(items.map((item) => [conversationItemKey(item), item]));
  const latest = turns.at(-1);
  const scrollKey = latest ? `${latest.id}:${latest.resultKeys.length}:${latest.pendingCount}` : '';

  // Follow new results only while the user is already at the bottom, so
  // reading older results is never interrupted.
  const atBottom = useRef(true);
  useEffect(() => {
    const element = scrollRef.current;
    if (element && atBottom.current) {
      element.scrollTo({ top: element.scrollHeight, behavior: 'smooth' });
    }
  }, [scrollKey]);

  return (
    <div
      className="flow-workspace-gallery flow-conversation-gallery"
      ref={scrollRef}
      onScroll={(event) => {
        const el = event.currentTarget;
        atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_SLACK_PX;
      }}
    >
      {turns.length === 0 ? (
        <div className="flow-gallery-empty">
          <Icon icon={Sparkles} size="display" className="flow-empty-icon" />
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
                                <img
                                  src={attachment.url}
                                  alt={attachment.name}
                                  loading="lazy"
                                  decoding="async"
                                />
                              ) : (
                                <>
                                  <video src={attachment.url} muted preload="metadata" />
                                  <Icon
                                    icon={Video}
                                    size="md"
                                    className="flow-chat-attachment-video-icon"
                                  />
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
                      <Icon icon={Sparkles} size="md" />
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
                            <Icon icon={RefreshCw} size="xl" className="skeleton-spinner" />
                            <span>
                              {turn.pendingLabel} {index + 1}/{turn.pendingCount}…
                            </span>
                          </div>
                        ))}
                        {turn.pendingCount > 0 && onStopTurn && (
                          <button
                            type="button"
                            className="flow-card-action-btn flow-card-text-btn flow-chat-stop"
                            title="Dừng chờ. Job Flow đã nhận vẫn chạy và có thể kiểm tra tiếp."
                            onClick={() => onStopTurn(turn.id)}
                          >
                            <Icon icon={Square} size="sm" />
                            Dừng
                          </button>
                        )}
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
      {preview && (
        <Modal variant="lightbox" label={preview.name} onClose={() => setPreview(null)}>
          {preview.kind === 'image' ? (
            <img src={preview.url} alt={preview.name} decoding="async" />
          ) : (
            <video src={preview.url} controls preload="metadata" />
          )}
          <span className="modal-caption">{preview.name}</span>
        </Modal>
      )}
    </div>
  );
}
