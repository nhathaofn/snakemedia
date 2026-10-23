import { useEffect } from 'react';
import { api } from '../services';
import { useAppStore } from '../stores';
import { AppEvent } from '../types';
import type { AppConfig } from '../types';
import { useWailsEvent } from './useWailsEvent';

/**
 * Loads backend-owned state (app identity, configuration) into the store once,
 * and keeps configuration in sync with backend `config:updated` events.
 */
export function useAppBootstrap(): void {
  const setAppInfo = useAppStore((s) => s.setAppInfo);
  const setConfig = useAppStore((s) => s.setConfig);
  const addToast = useAppStore((s) => s.addToast);

  useEffect(() => {
    let cancelled = false;

    void Promise.all([api.getAppInfo(), api.getConfig()]).then(([infoRes, cfgRes]) => {
      if (cancelled) return;
      if (infoRes.success && infoRes.data) {
        setAppInfo(infoRes.data);
        document.title = infoRes.data.productName;
      }
      if (cfgRes.success && cfgRes.data) {
        setConfig(cfgRes.data);
      }
      const failure = [infoRes, cfgRes].find((r) => !r.success);
      if (failure) {
        addToast({ type: 'error', title: 'Backend unavailable', message: failure.error ?? '' });
      }
    });

    return () => {
      cancelled = true;
    };
  }, [setAppInfo, setConfig, addToast]);

  useWailsEvent<AppConfig>(AppEvent.ConfigUpdated, setConfig);
}
