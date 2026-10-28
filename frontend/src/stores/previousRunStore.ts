import { create } from 'zustand';

/** When this run of the app started, in the backend's timestamp form (UTC). */
export const RUN_START = new Date().toISOString();

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
