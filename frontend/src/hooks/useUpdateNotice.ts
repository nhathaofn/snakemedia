import { useCallback } from 'react';
import { UPDATE_TOAST_DURATION_MS } from '../constants';
import { useAppStore } from '../stores';
import { AppEvent } from '../types';
import type { UpdateRelease } from '../types';
import { useWailsEvent } from './useWailsEvent';

/**
 * Tells the user once about a newer release the backend found at startup and
 * keeps it for Cài đặt → Chung, where it is installed.
 */
export function useUpdateNotice(): void {
  const setAvailableUpdate = useAppStore((s) => s.setAvailableUpdate);
  const addToast = useAppStore((s) => s.addToast);

  const onAvailable = useCallback(
    (release: UpdateRelease) => {
      setAvailableUpdate(release);
      addToast({
        type: 'info',
        title: `Có bản mới ${release.latest}`,
        message: 'Vào Cài đặt → Chung → Cập nhật để cài đặt.',
        durationMs: UPDATE_TOAST_DURATION_MS,
      });
    },
    [setAvailableUpdate, addToast],
  );

  useWailsEvent<UpdateRelease>(AppEvent.UpdateAvailable, onAvailable);
}
