import { create } from 'zustand';

/** When this run of the app started, in the backend's timestamp form (UTC). */
export const RUN_START = new Date().toISOString();

const LAST_RUN_KEY = 'snakemedia.lastRunStart';

/**
 * When the run before this one started; RUN_START when unknown (first start),
 * so nothing counts as the earlier run. Only that one run is offered again:
 * results of runs before it stay in the Library, off the create screens.
 */
export const PREVIOUS_RUN_START = (() => {
  let previous: string | null = null;
  try {
    previous = localStorage.getItem(LAST_RUN_KEY);
    localStorage.setItem(LAST_RUN_KEY, RUN_START);
  } catch {
    // Storage unavailable: treat this as a first start.
  }
  return previous && !Number.isNaN(Date.parse(previous)) ? previous : RUN_START;
})();

/** Which run of the app a result belongs to. */
export type RunAge = 'current' | 'previous' | 'older';

/**
 * The run a result belongs to from its time. Unfinished results and ones
 * without a time are always this run's, so a paid job never leaves the screen.
 */
export function runAge(
  time: string | undefined,
  finished: boolean,
  start = RUN_START,
  previousStart = PREVIOUS_RUN_START,
): RunAge {
  const at = Date.parse(time ?? '');
  if (!finished || Number.isNaN(at) || at >= Date.parse(start)) return 'current';
  return at >= Date.parse(previousStart) ? 'previous' : 'older';
}

interface PreviousRunState {
  /**
   * Whether results finished in an earlier run of the app are shown on the
   * create screens: null until the user answers the question asked on start.
   * Hidden results are never deleted.
   */
  showPrevious: boolean | null;
  setShowPrevious: (show: boolean) => void;
  /**
   * What each provider's earlier run left, as phrases for the question
   * ("3 ảnh Muse"): null while that provider is still reading it.
   */
  reports: Record<string, string[] | null>;
  report: (source: string, parts: string[] | null) => void;
}

/** The user's choice about the earlier run's results, shared by every provider. */
export const usePreviousRunStore = create<PreviousRunState>((set) => ({
  showPrevious: null,
  setShowPrevious: (show) => set({ showPrevious: show }),
  reports: {},
  report: (source, parts) => set((s) => ({ reports: { ...s.reports, [source]: parts } })),
}));

/** How a screen's results are told apart, for latestOfPreviousRun. */
export interface RunItemOf<T> {
  id: (item: T) => string;
  /** When the item ended (was made, when unknown): decides its run. */
  time: (item: T) => string | undefined;
  /** When the item was made: the last task is the one made last. Defaults to time. */
  made?: (item: T) => string | undefined;
  finished: (item: T) => boolean;
  /** The screen that lists it: each screen offers its own last task. */
  screen: (item: T) => string;
  /** The task it is part of: a batch (one prompt list) or one single job. */
  task: (item: T) => string;
}

/**
 * The results the start question offers again: on each screen, only the last
 * task made in the run before this one that it finished (a single job, or
 * every result of the last batch). Anything else from earlier runs stays off the create screens.
 */
export function latestOfPreviousRun<T>(
  items: readonly T[],
  of: RunItemOf<T>,
  start?: string,
  previousStart?: string,
): Set<string> {
  const previous = items.filter(
    (item) => runAge(of.time(item), of.finished(item), start, previousStart) === 'previous',
  );
  const last = new Map<string, { task: string; at: number }>();
  for (const item of previous) {
    const at = Date.parse((of.made ?? of.time)(item) ?? '');
    const screen = of.screen(item);
    const seen = last.get(screen);
    if (!seen || at > seen.at) last.set(screen, { task: of.task(item), at });
  }
  return new Set(
    previous
      .filter((item) => last.get(of.screen(item))?.task === of.task(item))
      .map((item) => of.id(item)),
  );
}

/** Whether a result is listed: this run's always, the offered ones while shown. */
export function shownForRun(
  age: RunAge,
  id: string,
  offered: ReadonlySet<string>,
  showPrevious: boolean | null,
): boolean {
  return age === 'current' || (!!showPrevious && offered.has(id));
}
