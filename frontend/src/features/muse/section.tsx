import {
  Film,
  FolderKanban,
  ScrollText,
  Settings,
  Shield,
  Sparkles,
  User,
  Wand2,
} from 'lucide-react';
import type { AppSection } from '../../types';
import { MuseAccountsPage } from './accounts/MuseAccountsPage';
import { MuseCreatePage } from './create/MuseCreatePage';
import { MuseLibraryPage } from './library/MuseLibraryPage';
import { MuseProxyPage } from './proxy/MuseProxyPage';
import { MuseScriptPage } from './script/MuseScriptPage';
import { MuseSettingsPage } from './settings/MuseSettingsPage';
import { MuseStudioPage } from './studio/MuseStudioPage';

/** Muse tab: batch image/video generation on muse.ai across several accounts. */
export const museSection = {
  id: 'muse',
  path: '/muse',
  label: 'Muse',
  icon: Sparkles,
  features: [
    { id: 'create', path: 'create', label: 'Tạo', icon: Wand2, element: <MuseCreatePage /> },
    {
      id: 'script',
      path: 'script',
      label: 'Kịch bản',
      icon: ScrollText,
      element: <MuseScriptPage />,
    },
    {
      id: 'studio',
      path: 'studio',
      label: 'Studio & Tư liệu',
      icon: FolderKanban,
      element: <MuseStudioPage />,
    },
    {
      id: 'library',
      path: 'library',
      label: 'Thư viện',
      icon: Film,
      element: <MuseLibraryPage />,
    },
    {
      id: 'accounts',
      path: 'accounts',
      label: 'Tài khoản Muse',
      icon: User,
      element: <MuseAccountsPage />,
    },
    {
      id: 'proxy',
      path: 'proxy',
      label: 'Proxy',
      icon: Shield,
      element: <MuseProxyPage />,
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
