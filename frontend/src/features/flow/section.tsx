import { Clapperboard, Image, Scissors, Settings, Workflow } from 'lucide-react';
import { FlowPreviousRunReport } from './shared/PreviousRunReport';
import { VideoToolsPrompt } from './shared/VideoToolsPrompt';
import type { AppSection } from '../../types';
import { lazyPage } from '../../routes/lazyPage';

const FlowEditPage = lazyPage(() => import('./edit/FlowEditPage').then((m) => m.FlowEditPage));
const FlowImagePage = lazyPage(() => import('./image/FlowImagePage').then((m) => m.FlowImagePage));
const FlowSettingsPage = lazyPage(() =>
  import('./settings/FlowSettingsPage').then((m) => m.FlowSettingsPage),
);
const FlowVideoPage = lazyPage(() => import('./video/FlowVideoPage').then((m) => m.FlowVideoPage));

/** Flow tab: Google Flow workspace tools. */
export const flowSection = {
  id: 'flow',
  path: '/flow',
  label: 'Flow',
  icon: Workflow,
  // Tells the start question what the earlier run left in Chỉnh sửa video,
  // and asks for FFmpeg when the video watermark removal cannot run.
  startup: (
    <>
      <FlowPreviousRunReport />
      <VideoToolsPrompt />
    </>
  ),
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
