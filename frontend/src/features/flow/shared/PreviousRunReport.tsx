import { useReportPreviousEdits } from './recovery';

/** Reports the earlier run's video edits to the start question; renders nothing. */
export function FlowPreviousRunReport() {
  useReportPreviousEdits();
  return null;
}
