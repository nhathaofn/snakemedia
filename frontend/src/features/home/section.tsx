import { House, LayoutDashboard, User } from 'lucide-react';
import type { AppSection } from '../../types';
import { AccountsPage } from './accounts';
import { HomeOverviewPage } from './overview/HomeOverviewPage';

/** Home tab: workspace overview and account management. */
export const homeSection = {
  id: 'home',
  path: '/home',
  label: 'Home',
  icon: House,
  features: [
    {
      id: 'overview',
      path: 'overview',
      label: 'Overview',
      icon: LayoutDashboard,
      element: <HomeOverviewPage />,
    },
    { id: 'accounts', path: 'accounts', label: 'Account', icon: User, element: <AccountsPage /> },
  ],
} as const satisfies AppSection;
