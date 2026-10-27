/** The byte-order mark some editors put at the start of a .txt file. */
const BOM = String.fromCharCode(0xfeff);

/** Matches a blank line: a line break, optional spaces or tabs, another break. */
const BLANK_LINE = /\n[\t ]*\n/;

/**
 * The one rule every batch form (Flow, Muse) uses to turn pasted text into
 * prompts:
 *  - when the text contains a blank line, prompts are the blocks between blank
 *    lines, so a prompt may span several lines;
 *  - otherwise every non-empty line is its own prompt.
 * Surrounding whitespace is trimmed and empty prompts are dropped.
 */
export function splitPromptList(text: string): string[] {
  const normalized = (text.startsWith(BOM) ? text.slice(1) : text).replace(/\r\n?/g, '\n');
  const parts = BLANK_LINE.test(normalized)
    ? normalized.split(/\n(?:[\t ]*\n)+/)
    : normalized.split('\n');
  return parts.map((prompt) => prompt.trim()).filter(Boolean);
}

/** Repeats every prompt `repeat` times in place (a, a, b, b), for queues without a repeat field. */
export function repeatPrompts(prompts: readonly string[], repeat: number): string[] {
  const times = Math.max(1, Math.floor(repeat));
  return prompts.flatMap((prompt) => Array.from({ length: times }, () => prompt));
}
