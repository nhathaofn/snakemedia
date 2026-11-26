import { Fragment, useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { LIST_REVEAL_STEP } from '../../constants/ui';
import { Pagination } from '../ui';
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
 * in fixed columns. Pagination limits rendered rows even after scrolling.
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
  const [view, setView] = useState({ resetKey, page: 0 });
  const scroll = useRef<HTMLDivElement>(null);
  const pageCount = Math.ceil(items.length / LIST_REVEAL_STEP);
  const page = view.resetKey === resetKey ? Math.min(view.page, Math.max(0, pageCount - 1)) : 0;
  if (view.resetKey !== resetKey || view.page !== page) setView({ resetKey, page });
  useEffect(() => {
    scroll.current?.scrollTo({ top: 0 });
  }, [page, resetKey]);
  const start = page * LIST_REVEAL_STEP;

  return (
    <div className="queue-table-layout">
      <div className="queue-scroll" ref={scroll}>
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
            <div className="queue-table-body" role="rowgroup">
              {items.slice(start, start + LIST_REVEAL_STEP).map((item) => (
                <Fragment key={keyOf(item)}>{render(item)}</Fragment>
              ))}
            </div>
          )}
        </div>
      </div>
      <Pagination
        label="Phân trang hàng đợi"
        page={page}
        pageCount={pageCount}
        onPageChange={(next) => setView({ resetKey, page: next })}
      />
    </div>
  );
}
