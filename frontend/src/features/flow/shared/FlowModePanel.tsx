import type { ReactNode } from 'react';
import { AccountWarning } from '../../../components/accounts';
import { AccountProvider } from '../../../types';
import { useRotationCount } from './hooks';

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

/** Number of signed-in accounts that take turns generating, or where to add one. */
function RotationStatusBadge() {
  const count = useRotationCount();
  if (count === 0) return <AccountWarning provider={AccountProvider.Flow} compact />;
  return (
    <div
      className="rotation-status-badge has-accounts"
      title="Tự động luân phiên tài khoản để chia đều luồng tạo"
    >
      <span className="status-dot active" />
      <span>Xoay vòng: {count} tài khoản sẵn sàng</span>
    </div>
  );
}
