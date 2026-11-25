import { useState } from 'react';
import { ChevronLeft, ChevronRight, Copy } from 'lucide-react';
import { Button, Icon, Modal, SegmentedControl } from '../../../components/ui';
import { useAppStore } from '../../../stores';
import type { MusePromptPreview } from '../../../types';

/** Resolves active preview tab text for a prompt preview item. */
export function resolvePreviewTab(
  item: MusePromptPreview,
  tab: 'main' | 'storyboard',
): { activeTab: 'main' | 'storyboard'; bodyText: string; hasStoryboard: boolean } {
  const hasStoryboard = Boolean(item.storyboardMessage);
  const activeTab = hasStoryboard && tab === 'storyboard' ? 'storyboard' : 'main';
  const bodyText = activeTab === 'storyboard' ? (item.storyboardMessage ?? '') : item.message;
  return { activeTab, bodyText, hasStoryboard };
}

/** Body of the Muse batch message preview modal (isolated for headless SSR testing). */
export function MessagePreviewContent({
  items,
  index,
  tab,
  onIndexChange,
  onTabChange,
}: {
  items: readonly MusePromptPreview[];
  index: number;
  tab: 'main' | 'storyboard';
  onIndexChange: (next: number) => void;
  onTabChange: (next: 'main' | 'storyboard') => void;
}) {
  const safeIndex = Math.min(index, Math.max(items.length - 1, 0));
  const current = items[safeIndex];
  if (!current) return null;

  const { activeTab, bodyText, hasStoryboard } = resolvePreviewTab(current, tab);

  return (
    <div className="muse-preview-body">
      <div className="muse-preview-toolbar">
        <div className="muse-preview-meta">
          <span>{current.images} ảnh đính kèm</span>
          {current.estimatedBytes != null && current.estimatedBytes > 0 && (
            <span>
              ~{(current.estimatedBytes / (1024 * 1024)).toFixed(1)} /{' '}
              {Math.round((current.budgetBytes || 8 * 1024 * 1024) / (1024 * 1024))} MB (.jpg ≤
              2048px)
            </span>
          )}
          {current.tags.length > 0 && <span>Tư liệu: @{current.tags.join(', @')}</span>}
        </div>
        {items.length > 1 && (
          <div className="muse-preview-nav">
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-label="Prompt trước"
              disabled={safeIndex === 0}
              onClick={() => onIndexChange(Math.max(0, safeIndex - 1))}
            >
              <Icon icon={ChevronLeft} size="sm" />
            </Button>
            <span>
              {safeIndex + 1} / {items.length}
            </span>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              aria-label="Prompt tiếp"
              disabled={safeIndex >= items.length - 1}
              onClick={() => onIndexChange(Math.min(items.length - 1, safeIndex + 1))}
            >
              <Icon icon={ChevronRight} size="sm" />
            </Button>
          </div>
        )}
      </div>

      {hasStoryboard && (
        <SegmentedControl
          aria-label="Bước câu gửi"
          options={[
            { value: 'main', label: 'Video chính' },
            { value: 'storyboard', label: 'Ảnh Storyboard' },
          ]}
          value={activeTab}
          onChange={(next) => onTabChange(next as 'main' | 'storyboard')}
        />
      )}

      {current.warnings.length > 0 && (
        <div className="muse-preview-warnings">
          {current.warnings.map((w) => (
            <span key={w} className="studio-field-hint is-warning">
              {w}
            </span>
          ))}
        </div>
      )}

      <pre className="muse-message-preview-box">{bodyText}</pre>
    </div>
  );
}

/**
 * Compact dialog showing the exact composed message(s) that Hàng loạt will
 * send to Muse, including @tag resolution, format directives, and warnings.
 */
export function MessagePreviewModal({
  items,
  onClose,
}: {
  items: readonly MusePromptPreview[];
  onClose: () => void;
}) {
  const [index, setIndex] = useState(0);
  const [tab, setTab] = useState<'main' | 'storyboard'>('main');
  const addToast = useAppStore((s) => s.addToast);
  const safeIndex = Math.min(index, Math.max(items.length - 1, 0));
  const current = items[safeIndex];
  if (!current) return null;

  const { bodyText } = resolvePreviewTab(current, tab);

  const copy = () => {
    void navigator.clipboard
      .writeText(bodyText)
      .then(() => addToast({ type: 'success', message: 'Đã chép nội dung câu gửi.' }))
      .catch(() => addToast({ type: 'error', message: 'Không chép được nội dung.' }));
  };

  return (
    <Modal
      title="Xem trước câu gửi Muse"
      subtitle="Kiểm tra nội dung thực tế gửi vào chat Muse (không tạo tác vụ)"
      className="muse-preview-modal"
      onClose={onClose}
      footer={
        <div className="muse-dialog-actions">
          <Button size="sm" onClick={copy}>
            <Icon icon={Copy} size="sm" />
            <span>Sao chép</span>
          </Button>
          <Button size="sm" variant="primary" onClick={onClose}>
            Đóng
          </Button>
        </div>
      }
    >
      <MessagePreviewContent
        items={items}
        index={index}
        tab={tab}
        onIndexChange={setIndex}
        onTabChange={setTab}
      />
    </Modal>
  );
}
