import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';
import { Layers } from 'lucide-react';
import { api } from '../../../services/api';

/**
 * Tools panel of one mode. Fields stack from the top; the account rotation status
 * can be hidden when the mode has its own footer action.
 */
export function FlowModePanel({
  children,
  showRotationStatus = true,
  className,
}: {
  children: ReactNode;
  showRotationStatus?: boolean;
  className?: string;
}) {
  return (
    <div className={`flow-mode-panel${className ? ` ${className}` : ''}`}>
      {children}
      {showRotationStatus && <RotationStatusBadge />}
    </div>
  );
}

/** Number of connected accounts with valid cookies that take turns generating. */
function RotationStatusBadge() {
  const [count, setCount] = useState(0);

  useEffect(() => {
    let active = true;
    void api.getActiveRotationCount().then((res) => {
      if (active && res.success && typeof res.data === 'number') setCount(res.data);
    });
    return () => {
      active = false;
    };
  }, []);

  return (
    <div
      className={`rotation-status-badge ${count > 0 ? 'has-accounts' : 'no-accounts'}`}
      title="Tự động luân phiên tài khoản để chia đều luồng tạo"
    >
      <span className={`status-dot ${count > 0 ? 'active' : 'inactive'}`} />
      <span>
        {count > 0
          ? `Xoay vòng: ${count} tài khoản sẵn sàng`
          : 'Chưa có tài khoản có cookie hợp lệ'}
      </span>
    </div>
  );
}

/** Content of a mode that is not built yet. */
export function ModePlaceholder({ text }: { text: string }) {
  return (
    <div className="flow-mode-placeholder">
      <Layers size={28} />
      <p>{text}</p>
    </div>
  );
}
