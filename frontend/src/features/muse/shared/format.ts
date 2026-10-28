import { MuseJobKind, MuseJobMode, MuseJobStatus } from '../../../types';
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

/** The screen a job belongs to; jobs saved before modes existed are Hàng loạt. */
export function jobMode(job: MuseJob): MuseJobMode {
  return job.mode || MuseJobMode.Batch;
}

/** The jobs of one studio screen (kind and mode), in the store's order. */
export function jobsOf(jobs: readonly MuseJob[], kind: MuseJobKind, mode: MuseJobMode): MuseJob[] {
  return jobs.filter((job) => screenKind(job) === kind && jobMode(job) === mode);
}

/** The screen kind of a job: a storyboard's image job belongs to its video. */
export function screenKind(job: MuseJob): MuseJobKind {
  return job.kind === MuseJobKind.Image && (job.boardFrames ?? 0) > 0
    ? MuseJobKind.Video
    : job.kind;
}

/** Queue filter: every job, one status, or the jobs on hold. */
export type QueueFilter = 'all' | MuseJobStatus | 'held';

/** Whether job shows under filter. */
export function matchesFilter(job: MuseJob, filter: QueueFilter): boolean {
  if (filter === 'all') return true;
  if (filter === 'held') return !!job.held;
  return job.status === filter;
}

/** Whether the job is waiting for or running on Muse. */
export function isActive(job: MuseJob): boolean {
  return job.status === MuseJobStatus.Queued || job.status === MuseJobStatus.Processing;
}

/**
 * Whether Muse took (or may have taken) the job's last send, so running it
 * again costs another turn. Mirrors the backend's mayBePaid.
 */
export function mayBePaid(job: MuseJob): boolean {
  return !!job.sessionId || !!job.sendUnknown;
}

/**
 * Whether a job belongs to an earlier run of the app: it ended before this
 * run started (made before it, for a job without an end time). Jobs still
 * waiting or running, and older jobs resumed or retried that ended in this
 * run, count as this run's, so hiding the earlier run never hides them.
 */
export function fromPreviousRun(job: MuseJob, runStart: string): boolean {
  const finished =
    job.status === MuseJobStatus.Succeeded ||
    job.status === MuseJobStatus.Failed ||
    job.status === MuseJobStatus.Cancelled;
  const made = Date.parse(job.finishedAt || job.createdAt);
  return finished && !Number.isNaN(made) && made < Date.parse(runStart);
}

/** Whether the job ended without files (failed or cancelled). */
export function isEnded(job: MuseJob): boolean {
  return job.status === MuseJobStatus.Failed || job.status === MuseJobStatus.Cancelled;
}

/**
 * What a finished job's result differs in from what was asked: its size or
 * length (formatNote), and what Muse changed in a long video's prompt to get
 * it past its video tool (changeNote). Empty when nothing differs.
 */
export function jobNote(job: MuseJob): string {
  return [job.formatNote, job.changeNote ? `Muse đã đổi: ${job.changeNote}` : '']
    .filter(Boolean)
    .join(' ');
}
