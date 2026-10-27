import { Clapperboard, Image, Scissors, Settings, Workflow } from 'lucide-react';
import type { AppSection } from '../../types';
import { FlowEditPage } from './edit/FlowEditPage';
import { FlowImagePage } from './image/FlowImagePage';
import { FlowSettingsPage } from './settings/FlowSettingsPage';
import { FlowVideoPage } from './video/FlowVideoPage';

/** Flow tab: Google Flow workspace tools. */
export const flowSection = {
  id: 'flow',
  path: '/flow',
  label: 'Flow',
  icon: Workflow,
  features: [
    {
      id: 'image',
      path: 'image',
      label: 'Ảnh',
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
      label: 'Chỉnh sửa video',
      icon: Scissors,
      element: <FlowEditPage />,
      layout: 'full',
    },
    {
      id: 'settings',
      path: 'settings',
      label: 'Cài đặt Flow',
      icon: Settings,
      element: <FlowSettingsPage />,
    },
  ],
} as const satisfies AppSection;
