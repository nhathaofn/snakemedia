import { MuseJobStatus } from '../../../types';
import type { MuseJob } from '../../../types';

export const jobStatusLabels: Record<MuseJobStatus, string> = {
  [MuseJobStatus.Queued]: 'Chờ chạy',
  [MuseJobStatus.Processing]: 'Đang chạy',
  [MuseJobStatus.Succeeded]: 'Xong',
  [MuseJobStatus.Failed]: 'Lỗi',
  [MuseJobStatus.Cancelled]: 'Đã huỷ',
};

/** Replaces the job with the same ID, or puts a new job first (newest first). */
export function upsertJob(jobs: readonly MuseJob[], job: MuseJob): MuseJob[] {
  const index = jobs.findIndex((item) => item.id === job.id);
  if (index < 0) return [job, ...jobs];
  const next = jobs.slice();
  next[index] = job;
  return next;
}

/**
 * Applies the changes events made while a list call was in flight to that
 * call's snapshot: a job an event carried replaces (or adds to) the snapshot's
 * copy, and a job an event removed (null) stays removed. An older snapshot
 * therefore never undoes a newer event.
 */
export function mergeSnapshot(
  snapshot: readonly MuseJob[],
  touched: ReadonlyMap<string, MuseJob | null>,
): MuseJob[] {
  let jobs = snapshot.slice();
  touched.forEach((job, id) => {
    jobs = job ? upsertJob(jobs, job) : jobs.filter((item) => item.id !== id);
  });
  return jobs;
}

/**
 * IDs of the selected jobs that are on screen, in screen order. Bulk actions use
 * only these, so a job hidden by the filter (or that moved out of it) is never
 * run or deleted by a selection the user can no longer see.
 */
export function selectedShown(shown: readonly MuseJob[], selected: ReadonlySet<string>): string[] {
  return shown.filter((job) => selected.has(job.id)).map((job) => job.id);
}
