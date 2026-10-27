import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { Icon } from '../Icon/Icon';
import './Modal.css';

const focusable =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

export interface ModalProps {
  /** Called on Escape, a click on the backdrop or the close button. */
  onClose: () => void;
  /** Heading; without it, `label` names the dialog for assistive tech. */
  title?: ReactNode;
  subtitle?: ReactNode;
  label?: string;
  /** 'dialog' is a panel with a header; 'lightbox' shows one media item large. */
  variant?: 'dialog' | 'lightbox';
  className?: string;
  footer?: ReactNode;
  children: ReactNode;
}

/**
 * The one modal of the app: rendered in a portal over everything, closed by
 * Escape or the backdrop, with focus kept inside while open and returned to
 * the opener when it closes.
 */
export function Modal({
  onClose,
  title,
  subtitle,
  label,
  variant = 'dialog',
  className = '',
  footer,
  children,
}: ModalProps) {
  const panelRef = useRef<HTMLElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    const first = panel?.querySelector<HTMLElement>(focusable);
    (first ?? panel)?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        closeRef.current();
        return;
      }
      if (event.key !== 'Tab' || !panel) return;
      const items = Array.from(panel.querySelectorAll<HTMLElement>(focusable));
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const last = items[items.length - 1];
      if (event.shiftKey && document.activeElement === items[0]) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        items[0].focus();
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
      opener?.focus?.();
    };
  }, []);

  return createPortal(
    <div className={`modal-backdrop modal-${variant}`} onMouseDown={onClose}>
      <section
        ref={panelRef}
        className={`modal-panel ${className}`}
        role="dialog"
        aria-modal="true"
        aria-label={title ? undefined : label}
        aria-labelledby={title ? 'modal-title' : undefined}
        tabIndex={-1}
        onMouseDown={(event) => event.stopPropagation()}
      >
        {title ? (
          <header className="modal-header">
            <div>
              <h2 id="modal-title">{title}</h2>
              {subtitle && <p>{subtitle}</p>}
            </div>
            <button type="button" className="modal-close" aria-label="Đóng" onClick={onClose}>
              <Icon icon={X} size="lg" />
            </button>
          </header>
        ) : (
          <button
            type="button"
            className="modal-close modal-close-floating"
            aria-label="Đóng"
            onClick={onClose}
          >
            <Icon icon={X} size="lg" />
          </button>
        )}
        <div className="modal-body">{children}</div>
        {footer && <footer className="modal-footer">{footer}</footer>}
      </section>
    </div>,
    document.body,
  );
}
