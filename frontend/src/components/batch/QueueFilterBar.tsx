import type { ReactNode } from 'react';
import { SegmentedControl } from '../ui';

export interface QueueFilterOption<V extends string> {
  value: V;
  label: string;
  count: number;
}

/**
 * The bar above a batch queue table: status filters with their counts, and
 * the screen's own queue actions on the right.
 */
export function QueueFilterBar<V extends string>({
  options,
  value,
  onChange,
  actions,
}: {
  options: readonly QueueFilterOption<V>[];
  value: V;
  onChange: (value: V) => void;
  actions?: ReactNode;
}) {
  return (
    <div className="queue-bar">
      <SegmentedControl
        options={options.map((o) => ({ value: o.value, label: `${o.label} ${o.count}` }))}
        value={value}
        onChange={onChange}
        aria-label="Lọc hàng đợi"
      />
      {actions && <div className="queue-bar-actions">{actions}</div>}
    </div>
  );
}
