import type { ReactNode } from 'react';
import { create } from 'zustand';
import './FlowStudio.css';
import { Tabs } from '../../../components/ui';
import type { TabItem } from '../../../components/ui';

export interface FlowStudioLayoutProps<T extends string> {
  /** Used for accessible names, e.g. "Image" or "Video". */
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
 * Two-column studio layout shared by every Flow media page: a fixed-width tools
 * panel with mode tabs (Single / All) on the left and the selected workspace on the right.
 * Pages using it are registered with `layout: 'full'`.
 */
export function FlowStudioLayout<T extends string>({
  label,
  modes,
  workspace,
  renderWorkspace,
}: FlowStudioLayoutProps<T>) {
  const saved = useStudioModes((state) => state.modes[label]);
  const setSaved = useStudioModes((state) => state.setMode);
  const mode = modes.find((item) => item.value === saved)?.value ?? modes[0].value;
  const setMode = (next: T) => setSaved(label, next);

  return (
    <div className="flow-studio-page">
      <aside className="flow-studio-sidebar" aria-label={`${label} tools`}>
        <Tabs aria-label={`${label} mode`} items={modes} value={mode} onChange={setMode} />
      </aside>
      <section className="flow-studio-content" aria-label={`${label} workspace`}>
        {renderWorkspace
          ? modes.map((item) => (
              <div
                className="flow-studio-workspace-panel"
                key={item.value}
                hidden={mode !== item.value}
              >
                {renderWorkspace(item.value)}
              </div>
            ))
          : workspace}
      </section>
    </div>
  );
}
