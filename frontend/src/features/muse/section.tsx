import { Clapperboard, Film, Image, Settings, Sparkles } from 'lucide-react';
import type { AppSection } from '../../types';
import { MuseJobKind } from '../../types';
import { MuseLibraryPage } from './library/MuseLibraryPage';
import { MuseSettingsPage } from './settings/MuseSettingsPage';
import { MuseWorkspace } from './shared/MuseWorkspace';

/** Muse tab: batch image and video generation with Muse's agent across the Muse accounts. */
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
      element: <MuseWorkspace kind={MuseJobKind.Image} />,
    },
    {
      id: 'video',
      path: 'video',
      label: 'Video',
      icon: Clapperboard,
      element: <MuseWorkspace kind={MuseJobKind.Video} />,
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
