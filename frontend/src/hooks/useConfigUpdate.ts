import { useCallback, useState } from 'react';
import { api } from '../services';
import { useAppStore } from '../stores';
import { AppConfig, ResponseCode } from '../types';
import type { ApiResponse } from '../types';

/**
 * A config change: fixed values, or values derived from the latest config so
 * a retry after a conflict rebuilds them on top of what another screen saved.
 */
export type ConfigPatch = Partial<AppConfig> | ((config: AppConfig) => Partial<AppConfig>);

/**
 * Sends patch on top of current. The backend refuses a config built from a
 * stale revision (another screen saved meanwhile) and answers with the newest
 * one; the patch is then applied to that once more.
 */
export async function saveConfigPatch(
  current: AppConfig,
  patch: ConfigPatch,
  send: (cfg: AppConfig) => Promise<ApiResponse<AppConfig>>,
): Promise<ApiResponse<AppConfig>> {
  const build = (cfg: AppConfig) =>
    AppConfig.createFrom({ ...cfg, ...(typeof patch === 'function' ? patch(cfg) : patch) });
  const res = await send(build(current));
  if (!res.success && res.code === ResponseCode.Conflict && res.data) {
    return send(build(res.data));
  }
  return res;
}

/**
 * Persists a partial configuration change through the backend and updates the
 * store on success; failures surface as an error toast.
 */
export function useConfigUpdate() {
  const setConfig = useAppStore((s) => s.setConfig);
  const addToast = useAppStore((s) => s.addToast);
  const [saving, setSaving] = useState(false);

  const update = useCallback(
    async (patch: ConfigPatch): Promise<boolean> => {
      const current = useAppStore.getState().config;
      if (!current) return false;

      setSaving(true);
      const res = await saveConfigPatch(current, patch, api.updateConfig);
      setSaving(false);

      if (res.success && res.data) {
        setConfig(res.data);
        return true;
      }
      if (res.code === ResponseCode.Conflict && res.data) setConfig(res.data);
      addToast({ type: 'error', title: 'Không lưu được cài đặt', message: res.error ?? '' });
      return false;
    },
    [setConfig, addToast],
  );

  return { update, saving };
}
