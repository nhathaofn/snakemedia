import { LayoutDashboard, Workflow } from 'lucide-react';
import type { AppSection } from '../../types';
import { FlowOverviewPage } from './overview/FlowOverviewPage';

/** Flow tab: Google Flow workspace tools. */
export const flowSection = {
  id: 'flow',
  path: '/flow',
  label: 'Flow',
  icon: Workflow,
  features: [
    {
      id: 'overview',
      path: 'overview',
      label: 'Overview',
      icon: LayoutDashboard,
      element: <FlowOverviewPage />,
    },
  ],
} as const satisfies AppSection;
