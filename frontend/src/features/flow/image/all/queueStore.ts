import { useMemo } from 'react';
import { create } from 'zustand';
import { api, unwrap } from '../../../../services';
import { FlowImageTaskStatus } from '../../../../types';
import type { FlowImageTask } from '../../../../types';
import { latestOfPreviousRun, runAge, shownForRun, usePreviousRunStore } from '../../../../stores';
import type { RunItemOf } from '../../../../stores';

/** Replaces the task with the same ID, or appends a new one (queue order is oldest first). */
export function upsertTask(tasks: readonly FlowImageTask[], task: FlowImageTask): FlowImageTask[] {
  const index = tasks.findIndex((item) => item.id === task.id);
  if (index < 0) return [...tasks, task];
  const next = tasks.slice();
  next[index] = task;
  return next;
}

/**
 * Lays the events received while a list call was in flight over its snapshot
 * (null: removed), so an older snapshot never undoes a newer event.
 */
export function mergeTasks(
  snapshot: readonly FlowImageTask[],
  touched: ReadonlyMap<string, FlowImageTask | null>,
): FlowImageTask[] {
  let tasks = snapshot.slice();
  touched.forEach((task, id) => {
    tasks = task ? upsertTask(tasks, task) : tasks.filter((item) => item.id !== id);
  });
  return tasks;
}

/** Counts shown in the panel summary. */
export function countTasks(tasks: readonly FlowImageTask[]) {
  const counts = { running: 0, queued: 0, held: 0, succeeded: 0, failed: 0 };
  for (const task of tasks) {
    if (task.status === FlowImageTaskStatus.Running) counts.running++;
    else if (task.status === FlowImageTaskStatus.Queued) {
      if (task.held) counts.held++;
      else counts.queued++;
    } else if (task.status === FlowImageTaskStatus.Succeeded) counts.succeeded++;
    else counts.failed++;
  }
  return counts;
}

/** Queue filter: every task, one status, or the tasks on hold. */
export type TaskFilter = 'all' | 'held' | FlowImageTaskStatus;

/** The filters above the queue, in order (labels as on Muse's queue). */
export const TASK_FILTERS: readonly { value: TaskFilter; label: string }[] = [
  { value: 'all', label: 'Tất cả' },
  { value: FlowImageTaskStatus.Running, label: 'Đang chạy' },
  { value: FlowImageTaskStatus.Queued, label: 'Chờ chạy' },
  { value: 'held', label: 'Tạm dừng' },
  { value: FlowImageTaskStatus.Succeeded, label: 'Xong' },
  { value: FlowImageTaskStatus.Failed, label: 'Lỗi' },
];

/** Whether task shows under filter; a held task counts as Tạm dừng, not Chờ chạy. */
export function matchesTaskFilter(task: FlowImageTask, filter: TaskFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'held') return task.status === FlowImageTaskStatus.Queued && !!task.held;
  if (filter === FlowImageTaskStatus.Queued) {
    return task.status === FlowImageTaskStatus.Queued && !task.held;
  }
  return task.status === filter;
}

interface QueueState {
  tasks: FlowImageTask[];
  warning: string;
  loaded: boolean;
  loadError: string | null;
  /** Loads the queue; calls made while one is running share it. */
  load: () => Promise<void>;
  setTask: (task: FlowImageTask | null) => void;
  /** Applies tasks added at once (FlowImageTasksAdded). */
  addTasks: (tasks: readonly FlowImageTask[] | null) => void;
  /** Drops tasks removed or trimmed at once (FlowImageTasksRemoved). */
  removeTasks: (ids: readonly string[] | null) => void;
  dismissWarning: () => void;
}

const touched = new Map<string, FlowImageTask | null>();
let reload: Promise<void> | null = null;

/** The image queue (Ảnh → Hàng loạt) as the backend runs it, kept current by events. */
export const useImageQueueStore = create<QueueState>((set) => ({
  tasks: [],
  warning: '',
  loaded: false,
  loadError: null,
  load: () => {
    reload ??= (async () => {
      touched.clear();
      try {
        const state = unwrap(await api.getFlowImageTasks());
        set({
          tasks: mergeTasks(state.tasks ?? [], touched),
          warning: state.warning ?? '',
          loaded: true,
          loadError: null,
        });
      } catch (cause) {
        set({ loaded: true, loadError: cause instanceof Error ? cause.message : String(cause) });
      } finally {
        touched.clear();
        reload = null;
      }
    })();
    return reload;
  },
  setTask: (task) => {
    if (!task?.id) return;
    if (reload) touched.set(task.id, task);
    set((state) => ({ tasks: upsertTask(state.tasks, task) }));
  },
  addTasks: (added) => {
    if (!added?.length) return;
    if (reload) added.forEach((task) => touched.set(task.id, task));
    set((state) => ({ tasks: added.reduce(upsertTask, state.tasks) }));
  },
  removeTasks: (ids) => {
    if (!ids?.length) return;
    const gone = new Set(ids);
    if (reload) gone.forEach((id) => touched.set(id, null));
    set((state) => ({ tasks: state.tasks.filter((task) => !gone.has(task.id)) }));
  },
  dismissWarning: () => set({ warning: '' }),
}));

/** A queue task's run: tasks of one batch share its folder. */
const taskRunItem: RunItemOf<FlowImageTask> = {
  id: (task) => task.id,
  time: (task) => task.finishedAt || task.createdAt,
  made: (task) => task.createdAt,
  finished: (task) =>
    task.status !== FlowImageTaskStatus.Queued && task.status !== FlowImageTaskStatus.Running,
  screen: () => '',
  task: (task) => task.outputDir,
};

/** The tasks of the last batch the run before this one finished (latestOfPreviousRun). */
export function offeredTasks(
  tasks: readonly FlowImageTask[],
  runStart?: string,
  previousStart?: string,
): Set<string> {
  return latestOfPreviousRun(tasks, taskRunItem, runStart, previousStart);
}

/**
 * The tasks the queue lists: of the run before this one, only its last batch
 * and only when the user chose to see it; other earlier tasks stay hidden.
 */
export function useVisibleImageTasks(): { tasks: FlowImageTask[]; offered: number } {
  const tasks = useImageQueueStore((s) => s.tasks);
  const showPrevious = usePreviousRunStore((s) => s.showPrevious);
  const offered = useMemo(() => offeredTasks(tasks), [tasks]);
  return useMemo(
    () => ({
      tasks: tasks.filter((task) =>
        shownForRun(
          runAge(taskRunItem.time(task), taskRunItem.finished(task)),
          task.id,
          offered,
          showPrevious,
        ),
      ),
      offered: offered.size,
    }),
    [tasks, offered, showPrevious],
  );
}
