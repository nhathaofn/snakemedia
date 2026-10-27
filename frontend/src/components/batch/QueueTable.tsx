import type { ReactNode } from 'react';
import { LIST_REVEAL_STEP } from '../../constants/ui';
import { RevealList } from '../ui';
import './QueueTable.css';

interface QueueTableProps<T> {
  label: string;
  /** Header of the number column (the select-all checkbox). */
  selectAll: ReactNode;
  /** Headers of the other columns, in order. */
  headings: readonly string[];
  items: readonly T[];
  keyOf: (item: T) => string;
  /** One row: a `.queue-table-row` whose cells follow the headings. */
  render: (item: T) => ReactNode;
  /** Shown instead of rows when there are none. */
  empty: ReactNode;
  resetKey?: string;
}

/**
 * The batch queue both providers show: a sticky header and one row per prompt
 * in fixed columns (number, prompt, references, results, progress), revealed
 * in steps for long queues.
 */
export function QueueTable<T>({
  label,
  selectAll,
  headings,
  items,
  keyOf,
  render,
  empty,
  resetKey,
}: QueueTableProps<T>) {
  return (
    <div className="queue-scroll">
      <div className="queue-table" role="table" aria-label={label}>
        <div className="queue-table-header" role="row">
          <span className="queue-table-index-heading" role="columnheader">
            {selectAll}
          </span>
          {headings.map((heading) => (
            <span key={heading} role="columnheader">
              {heading}
            </span>
          ))}
        </div>
        {items.length === 0 ? (
          <div className="queue-table-body is-empty" role="rowgroup">
            <div className="queue-table-empty">{empty}</div>
          </div>
        ) : (
          <RevealList
            className="queue-table-body"
            items={items}
            keyOf={keyOf}
            step={LIST_REVEAL_STEP}
            resetKey={resetKey}
            render={render}
          />
        )}
      </div>
    </div>
  );
}
