import { useEffect } from 'react';
import { onNextFrame, useWailsEvent } from '../../../../hooks';
import { AppEvent } from '../../../../types';
import type { FlowVideoTask } from '../../../../types';
import { useVideoQueueStore } from './queueStore';

/** Loads the video queue on mount and keeps it current from backend events. */
export function useVideoQueueLive() {
  const load = useVideoQueueStore((s) => s.load);
  const setTask = useVideoQueueStore((s) => s.setTask);
  const addTasks = useVideoQueueStore((s) => s.addTasks);
  const removeTasks = useVideoQueueStore((s) => s.removeTasks);

  useEffect(() => {
    void load();
  }, [load]);

  // Events are applied once per frame: a busy queue re-renders at most once a frame.
  useWailsEvent<FlowVideoTask | null>(AppEvent.FlowVideoTaskUpdated, (task) =>
    onNextFrame(() => setTask(task)),
  );
  // Added and removed tasks come with the event: no reload of the whole queue.
  useWailsEvent<FlowVideoTask[] | null>(AppEvent.FlowVideoTasksAdded, (tasks) =>
    onNextFrame(() => addTasks(tasks)),
  );
  useWailsEvent<string[] | null>(AppEvent.FlowVideoTasksRemoved, (ids) =>
    onNextFrame(() => removeTasks(ids)),
  );
}
