import { useState } from 'react';
import type { ReactNode } from 'react';
import { ChevronDown } from 'lucide-react';
import { Icon } from '../ui';

/** A titled settings group of a batch panel, optionally collapsible (closed by default). */
export function ConfigGroup({
  title,
  collapsible = false,
  defaultOpen = false,
  badge,
  summary,
  children,
}: {
  title: string;
  collapsible?: boolean;
  defaultOpen?: boolean;
  badge?: number;
  /** Shown on the right of the header bar while collapsed. */
  summary?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(collapsible ? defaultOpen : true);
  return (
    <section className="batch-config-group">
      {collapsible ? (
        <button
          type="button"
          className={`batch-config-title batch-config-toggle${open ? '' : ' is-collapsed'}`}
          aria-expanded={open}
          onClick={() => setOpen((prev) => !prev)}
        >
          <span>{title}</span>
          {badge !== undefined && badge > 0 && <span className="batch-config-badge">{badge}</span>}
          {!open && summary && <span className="batch-config-summary">{summary}</span>}
          <Icon icon={ChevronDown} size="sm" className="batch-config-chevron" />
        </button>
      ) : (
        <h3 className="batch-config-title">{title}</h3>
      )}
      {open && <div className="batch-config-content">{children}</div>}
    </section>
  );
}
