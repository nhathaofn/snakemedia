/**
 * How many items a reveal list shows. It starts with one step, grows a step
 * at a time as the user scrolls near its end, and never shows more than there
 * are (or fewer than one step while there are enough).
 */
export function revealCount(current: number, total: number, step: number): number {
  const size = Math.max(1, Math.floor(step));
  const atLeast = Math.max(current, size);
  return Math.max(0, Math.min(total, atLeast));
}

/** The count after one more step, capped at the total. */
export function nextRevealCount(current: number, total: number, step: number): number {
  return revealCount(
    revealCount(current, total, step) + Math.max(1, Math.floor(step)),
    total,
    step,
  );
}
