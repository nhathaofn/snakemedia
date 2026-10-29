import { useEffect, useRef } from 'react';
import { usePreviousRunStore } from '../../../stores';
import { offeredTasks, useImageQueueStore } from '../image/all/queueStore';
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
    const count = offeredTasks(tasks).size;
    report('flow-image', count > 0 ? [`${count} ảnh hàng loạt Flow`] : []);
  }, [loaded, load, report, tasks]);
}

/** Reports the earlier run's Flow results to the start question; renders nothing. */
export function FlowPreviousRunReport() {
  useReportPreviousEdits();
  useReportPreviousImageBatch();
  return null;
}
