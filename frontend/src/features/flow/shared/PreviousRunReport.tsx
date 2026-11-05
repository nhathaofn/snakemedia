import { useEffect, useRef } from 'react';
import { usePreviousRunStore } from '../../../stores';
import { offeredTasks as offeredImageTasks, useImageQueueStore } from '../image/all/queueStore';
import { offeredTasks as offeredVideoTasks, useVideoQueueStore } from '../video/all/queueStore';
import { useReportPreviousEdits } from './recovery';

/** Loads the image queue on start and reports its last earlier batch, once. */
function useReportPreviousImageBatch(): void {
  const load = useImageQueueStore((s) => s.load);
  const loaded = useImageQueueStore((s) => s.loaded);
  const tasks = useImageQueueStore((s) => s.tasks);
  const report = usePreviousRunStore((s) => s.report);
  const reported = useRef(false);
  useEffect(() => {
    if (!loaded) {
      report('flow-image', null);
      void load();
      return;
    }
    if (reported.current) return;
    reported.current = true;
    const count = offeredImageTasks(tasks).size;
    report('flow-image', count > 0 ? [`${count} ảnh hàng loạt Flow`] : []);
  }, [loaded, load, report, tasks]);
}

/** Loads the video queue on start and reports its last earlier batch, once. */
function useReportPreviousVideoBatch(): void {
  const load = useVideoQueueStore((s) => s.load);
  const loaded = useVideoQueueStore((s) => s.loaded);
  const tasks = useVideoQueueStore((s) => s.tasks);
  const report = usePreviousRunStore((s) => s.report);
  const reported = useRef(false);
  useEffect(() => {
    if (!loaded) {
      report('flow-video', null);
      void load();
      return;
    }
    if (reported.current) return;
    reported.current = true;
    const count = offeredVideoTasks(tasks).size;
    report('flow-video', count > 0 ? [`${count} video hàng loạt Flow`] : []);
  }, [loaded, load, report, tasks]);
}

/** Reports the earlier run's Flow results to the start question; renders nothing. */
export function FlowPreviousRunReport() {
  useReportPreviousEdits();
  useReportPreviousImageBatch();
  useReportPreviousVideoBatch();
  return null;
}
