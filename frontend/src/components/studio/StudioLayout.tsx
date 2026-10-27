import type { ReactNode } from 'react';
import { create } from 'zustand';
import './Studio.css';
import { Tabs } from '../ui';
import type { TabItem } from '../ui';

export interface StudioLayoutProps<T extends string> {
  /** Used for accessible names, e.g. "Image" or "Muse Video"; also keys the remembered mode. */
  label: string;
  /** Mode tabs of the tools panel, in display order; the first one opens by default. */
  modes: readonly TabItem<T>[];
  /** Static workspace used when changing the mode should not change the main area. */
  workspace?: ReactNode;
  /** Mode-specific workspaces, when a mode changes the main area. */
  renderWorkspace?: (mode: T) => ReactNode;
}

/** Open mode per studio label, so returning to a page keeps the tab the user left. */
const useStudioModes = create<{
  modes: Record<string, string>;
  setMode: (label: string, mode: string) => void;
}>((set) => ({
  modes: {},
  setMode: (label, mode) => set((state) => ({ modes: { ...state.modes, [label]: mode } })),
}));

/**
 * Two-column studio layout shared by the Flow and Muse media pages: a fixed-width
 * tools panel with mode tabs (Đơn / Hàng loạt) on the left and the selected workspace
 * on the right. Pages using it are registered with `layout: 'full'`.
 */
export function StudioLayout<T extends string>({
  label,
  modes,
  workspace,
  renderWorkspace,
}: StudioLayoutProps<T>) {
  const saved = useStudioModes((state) => state.modes[label]);
  const setSaved = useStudioModes((state) => state.setMode);
  const mode = modes.find((item) => item.value === saved)?.value ?? modes[0].value;
  const setMode = (next: T) => setSaved(label, next);

  return (
    <div className="studio-page">
      <aside className="studio-sidebar" aria-label={`${label} tools`}>
        <Tabs aria-label={`${label} mode`} items={modes} value={mode} onChange={setMode} />
      </aside>
      <section className="studio-content" aria-label={`${label} workspace`}>
        {renderWorkspace
          ? modes.map((item) => (
              <div className="studio-workspace-panel" key={item.value} hidden={mode !== item.value}>
                {renderWorkspace(item.value)}
              </div>
            ))
          : workspace}
      </section>
    </div>
  );
}
