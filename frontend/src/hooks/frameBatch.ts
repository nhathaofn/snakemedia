let pending: (() => void)[] = [];
let scheduled = false;

const nextFrame: (run: () => void) => void =
  typeof requestAnimationFrame === 'function'
    ? (run) => requestAnimationFrame(() => run())
    : (run) => setTimeout(run, 16);

function flush() {
  const work = pending;
  pending = [];
  scheduled = false;
  // Store updates made here in one go render once (React batches them).
  for (const apply of work) apply();
}

/**
 * Runs apply on the next frame together with everything else scheduled before
 * it, in order. Queue events can arrive dozens per second while a batch runs;
 * applied one by one, each would re-filter and re-render the whole queue.
 */
export function onNextFrame(apply: () => void): void {
  pending.push(apply);
  if (scheduled) return;
  scheduled = true;
  nextFrame(flush);
}

export interface FrameBatcher<T> {
  (id: string, value: T): void;
  /** Applies all pending buffered updates immediately and empties the buffer. */
  flush: () => void;
  /** Drops a buffered update for id so a direct RPC mutation is not rolled back on the next frame. */
  clear: (id: string) => void;
  /** Drops all buffered updates. */
  clearAll: () => void;
}

export function collectByIdOnNextFrame<T>(
  apply: (changes: ReadonlyMap<string, T>) => void,
): FrameBatcher<T> {
  let changes = new Map<string, T>();
  let scheduledLocal = false;

  const drain = () => {
    scheduledLocal = false;
    if (changes.size === 0) return;
    const current = changes;
    changes = new Map();
    apply(current);
  };

  const enqueue = ((id: string, value: T) => {
    changes.set(id, value);
    if (!scheduledLocal) {
      scheduledLocal = true;
      onNextFrame(drain);
    }
  }) as FrameBatcher<T>;

  enqueue.flush = drain;
  enqueue.clear = (id: string) => {
    changes.delete(id);
  };
  enqueue.clearAll = () => {
    changes.clear();
  };

  return enqueue;
}
