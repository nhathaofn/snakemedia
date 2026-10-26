import { useCallback, useState } from 'react';
import { api } from '../services';
import { useAppStore } from '../stores';
import { AppConfig } from '../types';

/**
 * Persists a partial configuration change through the backend and updates the
 * store on success; failures surface as an error toast.
 */
export function useConfigUpdate() {
  const setConfig = useAppStore((s) => s.setConfig);
  const addToast = useAppStore((s) => s.addToast);
  const [saving, setSaving] = useState(false);

  const update = useCallback(
    async (patch: Partial<AppConfig>): Promise<boolean> => {
      const current = useAppStore.getState().config;
      if (!current) return false;

      setSaving(true);
      const res = await api.updateConfig(AppConfig.createFrom({ ...current, ...patch }));
      setSaving(false);

      if (res.success && res.data) {
        setConfig(res.data);
        return true;
      }
      addToast({ type: 'error', title: 'Không lưu được cài đặt', message: res.error ?? '' });
      return false;
    },
    [setConfig, addToast],
  );

  return { update, saving };
}
