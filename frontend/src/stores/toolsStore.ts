import { create } from 'zustand';
import { api, unwrapList } from '../services';
import type { Tool, ToolProgress, ToolRequest } from '../types';

interface ToolsState {
  /** Every external tool (internal/tools) and where its programs were found. */
  tools: Tool[];
  /** Features' asks for a missing tool the user has not answered. */
  requests: ToolRequest[];
  /** Running downloads by tool ID. */
  progress: Record<string, ToolProgress>;
  load: () => Promise<void>;
  ask: (request: ToolRequest) => void;
  setProgress: (progress: ToolProgress) => void;
  /** Downloads a tool; resolves to the error message, or null once it is ready. */
  install: (id: string) => Promise<string | null>;
  postpone: (id: string) => void;
  remove: (id: string) => Promise<string | null>;
}

const withoutKey = <T>(record: Record<string, T>, key: string): Record<string, T> =>
  Object.fromEntries(Object.entries(record).filter(([k]) => k !== key));

/** Shared tool state, kept current by backend events (see useToolsLive). */
export const useToolsStore = create<ToolsState>((set, get) => ({
  tools: [],
  requests: [],
  progress: {},
  load: async () => {
    const [list, activity] = await Promise.all([api.listTools(), api.getToolActivity()]);
    set({ tools: unwrapList(list) });
    if (activity.success && activity.data) {
      const { requests, downloads } = activity.data;
      set({
        requests: requests ?? [],
        progress: Object.fromEntries((downloads ?? []).map((p) => [p.tool, p])),
      });
    }
  },
  ask: (request) =>
    set((s) => ({
      requests: [...s.requests.filter((r) => r.tool.id !== request.tool.id), request],
    })),
  setProgress: (progress) =>
    set((s) => ({ progress: { ...s.progress, [progress.tool]: progress } })),
  install: async (id) => {
    set((s) => ({ progress: { ...s.progress, [id]: { tool: id, done: 0, total: 0 } } }));
    const res = await api.installTool(id);
    set((s) => ({ progress: withoutKey(s.progress, id) }));
    if (!res.success || !res.data) return res.error ?? 'Không tải được công cụ.';
    const tool = res.data;
    set((s) => ({
      tools: s.tools.map((t) => (t.id === id ? tool : t)),
      requests: s.requests.filter((r) => r.tool.id !== id),
    }));
    return null;
  },
  postpone: (id) => {
    set((s) => ({ requests: s.requests.filter((r) => r.tool.id !== id) }));
    void api.postponeTool(id);
  },
  remove: async (id) => {
    const res = await api.removeTool(id);
    await get().load();
    return res.success ? null : (res.error ?? 'Không gỡ được công cụ.');
  },
}));
