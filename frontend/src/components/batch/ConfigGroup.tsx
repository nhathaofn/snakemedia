import type { ReactNode } from 'react';

/** A titled settings group of a batch panel, always open, above the prompt box. */
export function ConfigGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="batch-config-group">
      <h3 className="batch-config-title">{title}</h3>
      <div className="batch-config-content">{children}</div>
    </section>
  );
}
