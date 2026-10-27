import { House, Shield, User } from 'lucide-react';
import type { AppSection } from '../../types';
import { AccountsPage } from './accounts';
import { ProxyPage } from './proxy/ProxyPage';

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
