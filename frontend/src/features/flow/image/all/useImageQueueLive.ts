import { useEffect } from 'react';
import { useWailsEvent } from '../../../../hooks';
import { AppEvent } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { useImageQueueStore } from './queueStore';

/** Loads the image queue on mount and keeps it current from backend events. */
export function useImageQueueLive() {
  const load = useImageQueueStore((s) => s.load);
  const setTask = useImageQueueStore((s) => s.setTask);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<FlowImageTask | null>(AppEvent.FlowImageTaskUpdated, setTask);
  // Added or removed tasks carry no payload: reload the queue.
  useWailsEvent(AppEvent.FlowImageTasksChanged, () => void load());
}
