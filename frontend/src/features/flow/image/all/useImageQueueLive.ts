import { useEffect } from 'react';
import { onNextFrame, useWailsEvent } from '../../../../hooks';
import { AppEvent } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { useImageQueueStore } from './queueStore';

/** Loads the image queue on mount and keeps it current from backend events. */
export function useImageQueueLive() {
  const load = useImageQueueStore((s) => s.load);
  const setTask = useImageQueueStore((s) => s.setTask);
  const addTasks = useImageQueueStore((s) => s.addTasks);
  const removeTasks = useImageQueueStore((s) => s.removeTasks);

  useEffect(() => {
    void load();
  }, [load]);

  // Events are applied once per frame: a busy queue re-renders at most once a frame.
  useWailsEvent<FlowImageTask | null>(AppEvent.FlowImageTaskUpdated, (task) =>
    onNextFrame(() => setTask(task)),
  );
  // Added and removed tasks come with the event: no reload of the whole queue.
  useWailsEvent<FlowImageTask[] | null>(AppEvent.FlowImageTasksAdded, (tasks) =>
    onNextFrame(() => addTasks(tasks)),
  );
  useWailsEvent<string[] | null>(AppEvent.FlowImageTasksRemoved, (ids) =>
    onNextFrame(() => removeTasks(ids)),
  );
}
