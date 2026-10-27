import { useNavigate } from 'react-router-dom';
// Import the registry module directly: routes/index depends on layout and pages.
import { ROUTES } from '../../routes/registry';
import { AccountProvider } from '../../types';
import { Button } from '../ui';
import './AccountWarning.css';

const providerNames: Record<AccountProvider, string> = {
  [AccountProvider.Flow]: 'Flow',
  [AccountProvider.Muse]: 'Muse',
};

/**
 * The warning every provider shows while none of its accounts can take work,
 * with a button to that provider's tab on Trang chủ → Tài khoản.
 */
export function AccountWarning({
  provider,
  message,
  compact = false,
}: {
  provider: AccountProvider;
  message?: string;
  /** Smaller variant for narrow tool panels. */
  compact?: boolean;
}) {
  const navigate = useNavigate();
  const name = providerNames[provider];
  return (
    <div role="status" className={`account-warning${compact ? ' account-warning-compact' : ''}`}>
      <span>{message ?? `Chưa có tài khoản ${name} nào sẵn sàng.`}</span>
      <Button size="sm" onClick={() => navigate(`${ROUTES.home.accounts}?provider=${provider}`)}>
        Mở tài khoản {name}
      </Button>
    </div>
  );
}
