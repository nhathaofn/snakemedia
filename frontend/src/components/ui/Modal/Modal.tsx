import { useEffect, useId, useRef } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { Icon } from '../Icon/Icon';
import './Modal.css';

const focusable =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

const openPanels: HTMLElement[] = [];

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
  const titleId = useId();
  const panelRef = useRef<HTMLElement>(null);
  const closeRef = useRef(onClose);
  useEffect(() => {
    closeRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (!panel) return;
    openPanels.push(panel);
    const focusableItems = () =>
      Array.from(panel.querySelectorAll<HTMLElement>(focusable)).filter(
        (item) => item.tabIndex >= 0 && item.getClientRects().length > 0,
      );
    (focusableItems()[0] ?? panel).focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (openPanels.at(-1) !== panel || event.defaultPrevented) return;
      if (event.key === 'Escape') {
        event.preventDefault();
        event.stopPropagation();
        closeRef.current();
        return;
      }
      if (event.key !== 'Tab') return;
      const items = focusableItems();
      if (items.length === 0) {
        event.preventDefault();
        return;
      }
      const last = items[items.length - 1];
      if (!items.includes(document.activeElement as HTMLElement)) {
        event.preventDefault();
        (event.shiftKey ? last : items[0]).focus();
      } else if (event.shiftKey && document.activeElement === items[0]) {
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
      const wasTop = openPanels.at(-1) === panel;
      openPanels.splice(openPanels.indexOf(panel), 1);
      const next = openPanels.at(-1);
      if (wasTop && (!next || next.contains(opener))) opener?.focus();
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
        aria-labelledby={title ? titleId : undefined}
        tabIndex={-1}
        onMouseDown={(event) => event.stopPropagation()}
      >
        {title ? (
          <header className="modal-header">
            <div>
              <h2 id={titleId}>{title}</h2>
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
