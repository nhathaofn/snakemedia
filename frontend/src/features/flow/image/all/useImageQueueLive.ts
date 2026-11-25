import { useEffect } from 'react';
import { collectByIdOnNextFrame, useWailsEvent } from '../../../../hooks';
import { AppEvent } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { useImageQueueStore } from './queueStore';

const queueChange = collectByIdOnNextFrame<FlowImageTask | null>((changes) =>
  useImageQueueStore.getState().applyEvents(changes),
);

/** Loads the image queue on mount and keeps it current from backend events. */
export function useImageQueueLive() {
  const load = useImageQueueStore((s) => s.load);

  useEffect(() => {
    void load();
  }, [load]);

  // Events are applied once per frame: a busy queue re-renders at most once a frame.
  useWailsEvent<FlowImageTask | null>(AppEvent.FlowImageTaskUpdated, (task) => {
    if (task?.id) queueChange(task.id, task);
  });
  // Added and removed tasks come with the event: no reload of the whole queue.
  useWailsEvent<FlowImageTask[] | null>(AppEvent.FlowImageTasksAdded, (tasks) =>
    tasks?.forEach((task) => queueChange(task.id, task)),
  );
  useWailsEvent<string[] | null>(AppEvent.FlowImageTasksRemoved, (ids) =>
    ids?.forEach((id) => queueChange(id, null)),
  );
}
