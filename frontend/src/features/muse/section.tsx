import { Clapperboard, Film, Image, Settings, Sparkles, Users } from 'lucide-react';
import type { AppSection } from '../../types';
import { MuseJobKind } from '../../types';
import { MusePreviousRunReport } from './shared/PreviousRun';
import { lazyPage } from '../../routes/lazyPage';

const MuseAssetsPage = lazyPage(() =>
  import('./assets/MuseAssetsPage').then((m) => m.MuseAssetsPage),
);
const MuseLibraryPage = lazyPage(() =>
  import('./library/MuseLibraryPage').then((m) => m.MuseLibraryPage),
);
const MuseStudio = lazyPage(() => import('./MuseStudio').then((m) => m.MuseStudio));
const MuseSettingsPage = lazyPage(() =>
  import('./settings/MuseSettingsPage').then((m) => m.MuseSettingsPage),
);

/** Muse tab: single and batch image and video generation with Muse's agent across the Muse accounts. */
export const museSection = {
  id: 'muse',
  path: '/muse',
  label: 'Muse',
  icon: Sparkles,
  // Tells the start question what the earlier run left in the Muse queue.
  startup: <MusePreviousRunReport />,
  features: [
    {
      id: 'image',
      path: 'image',
      label: 'Ảnh',
      icon: Image,
      element: <MuseStudio kind={MuseJobKind.Image} />,
      layout: 'full',
    },
    {
      id: 'video',
      path: 'video',
      label: 'Video',
      icon: Clapperboard,
      element: <MuseStudio kind={MuseJobKind.Video} />,
      layout: 'full',
    },
    {
      id: 'assets',
      path: 'assets',
      label: 'Tư liệu',
      icon: Users,
      element: <MuseAssetsPage />,
    },
    {
      id: 'library',
      path: 'library',
      label: 'Thư viện',
      icon: Film,
      element: <MuseLibraryPage />,
    },
    {
      id: 'settings',
      path: 'settings',
      label: 'Cài đặt Muse',
      icon: Settings,
      element: <MuseSettingsPage />,
    },
  ],
} as const satisfies AppSection;
