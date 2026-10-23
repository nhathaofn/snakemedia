import React from 'react';
import { X } from 'lucide-react';
import './Toaster.css';
import { useAppStore } from '../../stores';
import { Icon } from '../ui';

export const Toaster: React.FC = () => {
  const toasts = useAppStore((s) => s.toasts);
  const removeToast = useAppStore((s) => s.removeToast);

  if (toasts.length === 0) return null;

  return (
    <div className="toaster-container" aria-live="polite">
      {toasts.map((toast) => (
        <div key={toast.id} className={`toast toast-${toast.type}`} role="status">
          <div className="toast-content">
            {toast.title && <div className="toast-title">{toast.title}</div>}
            <div className="toast-message">{toast.message}</div>
          </div>
          <button
            className="toast-close"
            onClick={() => removeToast(toast.id)}
            aria-label="Dismiss notification"
          >
            <Icon icon={X} size="sm" />
          </button>
        </div>
      ))}
    </div>
  );
};
