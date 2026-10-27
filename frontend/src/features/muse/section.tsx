import { Clapperboard, Film, Image, Settings, Sparkles } from 'lucide-react';
import type { AppSection } from '../../types';
import { MuseJobKind } from '../../types';
import { MuseLibraryPage } from './library/MuseLibraryPage';
import { MuseStudio } from './MuseStudio';
import { MuseSettingsPage } from './settings/MuseSettingsPage';

/** Muse tab: single and batch image and video generation with Muse's agent across the Muse accounts. */
export const museSection = {
  id: 'muse',
  path: '/muse',
  label: 'Muse',
  icon: Sparkles,
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
