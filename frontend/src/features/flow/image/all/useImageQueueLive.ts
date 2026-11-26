import { useEffect } from 'react';
import { useWailsEvent } from '../../../../hooks';
import { AppEvent } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { queueFlowImageEvent, useImageQueueStore } from './queueStore';

/** Loads the image queue on mount and keeps it current from backend events. */
export function useImageQueueLive() {
  const load = useImageQueueStore((s) => s.load);

  useEffect(() => {
    void load();
  }, [load]);

  // Events are applied once per frame: a busy queue re-renders at most once a frame.
  useWailsEvent<FlowImageTask | null>(AppEvent.FlowImageTaskUpdated, (task) => {
    if (task?.id) queueFlowImageEvent(task.id, task);
  });
  // Added and removed tasks come with the event: no reload of the whole queue.
  useWailsEvent<FlowImageTask[] | null>(AppEvent.FlowImageTasksAdded, (tasks) =>
    tasks?.forEach((task) => queueFlowImageEvent(task.id, task)),
  );
  useWailsEvent<string[] | null>(AppEvent.FlowImageTasksRemoved, (ids) =>
    ids?.forEach((id) => queueFlowImageEvent(id, null)),
  );
}
