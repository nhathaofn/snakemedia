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
