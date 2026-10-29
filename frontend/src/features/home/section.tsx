import { House, Shield, User } from 'lucide-react';
import type { AppSection } from '../../types';
import { lazyPage } from '../../routes/lazyPage';

const AccountsPage = lazyPage(() => import('./accounts').then((m) => m.AccountsPage));
const ProxyPage = lazyPage(() => import('./proxy/ProxyPage').then((m) => m.ProxyPage));

/** Home tab: every provider's accounts and the shared proxy list. */
export const homeSection = {
  id: 'home',
  path: '/home',
  label: 'Trang chủ',
  icon: House,
  features: [
    { id: 'accounts', path: 'accounts', label: 'Tài khoản', icon: User, element: <AccountsPage /> },
    { id: 'proxy', path: 'proxy', label: 'Proxy', icon: Shield, element: <ProxyPage /> },
  ],
} as const satisfies AppSection;
