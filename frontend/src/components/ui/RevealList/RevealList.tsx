import React, { useEffect, useRef, useState } from 'react';
import { Button } from '../Button/Button';
import { nextRevealCount, revealCount } from './reveal';
import './RevealList.css';

export interface RevealListProps<T> {
  items: readonly T[];
  keyOf: (item: T) => string;
  render: (item: T) => React.ReactNode;
  /** How many items one step shows. */
  step: number;
  /** Changing it (a new filter) starts again from one step. */
  resetKey?: string;
  className?: string;
}

/**
 * Renders a long list a step at a time: the next step appears as the user
 * scrolls near the end (or clicks "Hiện thêm"), so thousands of items never
 * mount at once. Items should set `content-visibility: auto` so the shown ones
 * off screen skip layout and paint as well.
 */
export function RevealList<T>({
  items,
  keyOf,
  render,
  step,
  resetKey = '',
  className = '',
}: RevealListProps<T>) {
  const [state, setState] = useState({ key: resetKey, count: step });
  // A new reset key starts again from one step (React's "adjust state while
  // rendering" pattern, no effect needed).
  if (state.key !== resetKey) setState({ key: resetKey, count: step });
  const count = revealCount(state.key === resetKey ? state.count : step, items.length, step);
  const more = items.length - count;
  const sentinel = useRef<HTMLDivElement>(null);

  const total = items.length;
  const showMore = () => setState((s) => ({ ...s, count: nextRevealCount(s.count, total, step) }));

  useEffect(() => {
    const node = sentinel.current;
    if (!node || more <= 0 || typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting))
          setState((s) => ({ ...s, count: nextRevealCount(s.count, total, step) }));
      },
      { rootMargin: '0px 0px 600px 0px' },
    );
    observer.observe(node);
    return () => observer.disconnect();
  }, [more, count, total, step]);

  return (
    <>
      <div className={className}>
        {items.slice(0, count).map((item) => (
          <React.Fragment key={keyOf(item)}>{render(item)}</React.Fragment>
        ))}
      </div>
      {more > 0 && (
        <div ref={sentinel} className="reveal-more">
          <Button size="sm" variant="ghost" onClick={showMore}>
            Hiện thêm {Math.min(more, step)} / còn {more}
          </Button>
        </div>
      )}
    </>
  );
}
