import { useEffect } from 'react';
import {
  BookmarkPlus,
  ChevronLeft,
  ChevronRight,
  Copy,
  CornerUpLeft,
  Download,
  FolderOpen,
  Trash2,
} from 'lucide-react';
import { Button, Icon, Modal } from '../../../components/ui';
import { useAppStore } from '../../../stores';
import { MuseJobKind } from '../../../types';

export interface MuseLightboxItem {
  url: string;
  kind: MuseJobKind;
  prompt: string;
  title?: string;
  meta?: string[];
  note?: string;
  revealPath?: string;
}

interface MuseMediaLightboxProps {
  items: readonly MuseLightboxItem[];
  index: number;
  onIndexChange?: (index: number) => void;
  onClose: () => void;
  onSave?: (item: MuseLightboxItem) => void;
  onReveal?: (item: MuseLightboxItem) => void;
  onSaveToVault?: (item: MuseLightboxItem) => void;
  onReusePrompt?: (item: MuseLightboxItem) => void;
  onRemove?: (item: MuseLightboxItem) => void;
}

/**
 * Shared media lightbox across Muse Đơn, Hàng loạt, and Thư viện: shows large
 * preview with aspect clarity, optional multi-item navigation, metadata tags,
 * prompt copy, and direct file/vault actions without closing the viewer.
 */
export function MuseMediaLightbox({
  items,
  index,
  onIndexChange,
  onClose,
  onSave,
  onReveal,
  onSaveToVault,
  onReusePrompt,
  onRemove,
}: MuseMediaLightboxProps) {
  const addToast = useAppStore((s) => s.addToast);
  const safeIndex = Math.max(0, Math.min(index, items.length - 1));
  const current = items[safeIndex];
  const hasPrev = safeIndex > 0;
  const hasNext = safeIndex < items.length - 1;

  useEffect(() => {
    if (!current || items.length <= 1 || !onIndexChange) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'ArrowLeft' && hasPrev) {
        event.preventDefault();
        onIndexChange(safeIndex - 1);
      } else if (event.key === 'ArrowRight' && hasNext) {
        event.preventDefault();
        onIndexChange(safeIndex + 1);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [current, hasNext, hasPrev, items.length, onIndexChange, safeIndex]);

  if (!current) return null;

  const label = current.title || current.prompt || 'Xem kết quả';
  const copyPrompt = () => {
    if (!current.prompt) return;
    void navigator.clipboard
      .writeText(current.prompt)
      .then(() => addToast({ type: 'success', message: 'Đã chép prompt.' }))
      .catch(() => addToast({ type: 'error', message: 'Không chép được prompt.' }));
  };

  return (
    <Modal
      variant="lightbox"
      className="muse-lightbox-modal"
      label={label}
      onClose={onClose}
      footer={
        <div className="muse-lightbox-footer">
          <div className="muse-lightbox-info">
            <div className="muse-lightbox-badges">
              <span className="muse-lightbox-kind">
                {current.kind === MuseJobKind.Video ? 'Video' : 'Ảnh'}
              </span>
              {current.meta?.map((tag) => (
                <span key={tag} className="muse-lightbox-chip">
                  {tag}
                </span>
              ))}
              {items.length > 1 && (
                <span className="muse-lightbox-counter">
                  {safeIndex + 1}/{items.length}
                </span>
              )}
            </div>
            {current.prompt && (
              <p className="muse-lightbox-prompt" title={current.prompt}>
                {current.prompt}
              </p>
            )}
            {current.note && <p className="muse-lightbox-note">{current.note}</p>}
          </div>
          <div className="muse-lightbox-actions">
            {current.prompt && (
              <Button size="sm" variant="ghost" onClick={copyPrompt}>
                <Icon icon={Copy} size="sm" />
                <span>Chép prompt</span>
              </Button>
            )}
            {onReusePrompt && (
              <Button
                size="sm"
                variant="secondary"
                onClick={() => {
                  onReusePrompt(current);
                  onClose();
                }}
              >
                <Icon icon={CornerUpLeft} size="sm" />
                <span>Dùng lại prompt</span>
              </Button>
            )}
            {onSaveToVault && current.kind === MuseJobKind.Image && (
              <Button size="sm" variant="secondary" onClick={() => onSaveToVault(current)}>
                <Icon icon={BookmarkPlus} size="sm" />
                <span>Lưu vào Kho</span>
              </Button>
            )}
            {onReveal && (
              <Button size="sm" variant="outline" onClick={() => onReveal(current)}>
                <Icon icon={FolderOpen} size="sm" />
                <span>Mở thư mục</span>
              </Button>
            )}
            {onSave && (
              <Button size="sm" variant="primary" onClick={() => onSave(current)}>
                <Icon icon={Download} size="sm" />
                <span>Lưu file</span>
              </Button>
            )}
            {onRemove && (
              <Button size="sm" variant="ghost" onClick={() => onRemove(current)}>
                <Icon icon={Trash2} size="sm" />
                <span>Bỏ khỏi thư viện</span>
              </Button>
            )}
          </div>
        </div>
      }
    >
      <div className="muse-lightbox-stage">
        {items.length > 1 && onIndexChange && (
          <button
            type="button"
            className="muse-lightbox-nav is-prev"
            aria-label="Mục trước"
            disabled={!hasPrev}
            onClick={() => onIndexChange(safeIndex - 1)}
          >
            <Icon icon={ChevronLeft} size="lg" />
          </button>
        )}
        {current.kind === MuseJobKind.Video ? (
          <video key={current.url} src={current.url} controls autoPlay preload="metadata" />
        ) : (
          <img key={current.url} src={current.url} alt={label} decoding="async" />
        )}
        {items.length > 1 && onIndexChange && (
          <button
            type="button"
            className="muse-lightbox-nav is-next"
            aria-label="Mục sau"
            disabled={!hasNext}
            onClick={() => onIndexChange(safeIndex + 1)}
          >
            <Icon icon={ChevronRight} size="lg" />
          </button>
        )}
      </div>
    </Modal>
  );
}
