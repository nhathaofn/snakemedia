import { House, LayoutDashboard, User } from 'lucide-react';
import type { AppSection } from '../../types';
import { AccountsPage } from './accounts';
import { HomeOverviewPage } from './overview/HomeOverviewPage';

/** Home tab: workspace overview and account management. */
export const homeSection = {
  id: 'home',
  path: '/home',
  label: 'Trang chủ',
  icon: House,
  features: [
    {
      id: 'overview',
      path: 'overview',
      label: 'Tổng quan',
      icon: LayoutDashboard,
      element: <HomeOverviewPage />,
    },
    { id: 'accounts', path: 'accounts', label: 'Tài khoản', icon: User, element: <AccountsPage /> },
  ],
} as const satisfies AppSection;
