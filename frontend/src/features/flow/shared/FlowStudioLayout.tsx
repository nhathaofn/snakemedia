import { useState } from 'react';
import type { ReactNode } from 'react';
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
  const [mode, setMode] = useState<T>(modes[0].value);

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
