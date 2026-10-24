import { Clapperboard, Image, LayoutDashboard, Scissors, Workflow } from 'lucide-react';
import type { AppSection } from '../../types';
import { FlowEditPage } from './edit/FlowEditPage';
import { FlowImagePage } from './image/FlowImagePage';
import { FlowOverviewPage } from './overview/FlowOverviewPage';
import { FlowVideoPage } from './video/FlowVideoPage';

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
    {
      id: 'image',
      path: 'image',
      label: 'Image',
      icon: Image,
      element: <FlowImagePage />,
      layout: 'full',
    },
    {
      id: 'video',
      path: 'video',
      label: 'Video',
      icon: Clapperboard,
      element: <FlowVideoPage />,
      layout: 'full',
    },
    {
      id: 'edit',
      path: 'edit',
      label: 'Edit video',
      icon: Scissors,
      element: <FlowEditPage />,
      layout: 'full',
    },
  ],
} as const satisfies AppSection;
