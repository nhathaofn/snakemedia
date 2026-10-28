import { useCallback } from 'react';
import { useAppStore } from '../stores';
import type { AppConfig } from '../types';
import { useConfigUpdate } from './useConfigUpdate';

type SectionKey = 'accounts' | 'flow' | 'muse' | 'updates';

/**
 * One nested group of the backend config (accounts, flow, muse or updates) with a
 * patch that saves changed fields on top of the current values.
 */
export function useConfigSection<K extends SectionKey>(key: K) {
  const section = useAppStore((s) => s.config?.[key]);
  const { update, saving } = useConfigUpdate();

  const patch = useCallback(
    (change: Partial<AppConfig[K]>) => {
      if (!useAppStore.getState().config) return;
      // Built from the config being saved, so a retry after a conflict keeps
      // what another screen changed in this section.
      void update((cfg) => ({ [key]: { ...cfg[key], ...change } }) as Partial<AppConfig>);
    },
    [key, update],
  );

  return { section, patch, saving };
}
