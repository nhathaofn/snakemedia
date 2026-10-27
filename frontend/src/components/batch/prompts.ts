/** The byte-order mark some editors put at the start of a .txt file. */
const BOM = String.fromCharCode(0xfeff);

/** How pasted text is understood as prompts. */
export const PROMPT_MODES = ['line', 'block', 'single'] as const;
export type PromptMode = (typeof PROMPT_MODES)[number];

export const DEFAULT_PROMPT_MODE: PromptMode = 'line';

export const PROMPT_MODE_LABELS: Record<PromptMode, string> = {
  line: 'Từng dòng',
  block: 'Theo đoạn',
  single: 'Tổng hợp',
};

export const PROMPT_MODE_HINTS: Record<PromptMode, string> = {
  line: 'Mỗi dòng là một prompt riêng.',
  block: 'Các đoạn cách nhau bằng dòng trống; các dòng liền nhau trong một đoạn là một prompt.',
  single: 'Toàn bộ văn bản được gom thành đúng một prompt.',
};

export function isPromptMode(value: unknown): value is PromptMode {
  return PROMPT_MODES.some((mode) => mode === value);
}

/**
 * The one rule every batch form (Flow, Muse) uses to turn pasted text into
 * prompts, by mode:
 *  - line: every non-empty line is its own prompt;
 *  - block: prompts are the blocks between blank lines, so a prompt may span
 *    several lines;
 *  - single: the whole text is one prompt.
 * Surrounding whitespace is trimmed and empty prompts are dropped.
 */
export function splitPromptList(text: string, mode: PromptMode = DEFAULT_PROMPT_MODE): string[] {
  const normalized = (text.startsWith(BOM) ? text.slice(1) : text).replace(/\r\n?/g, '\n');
  let parts: string[];
  switch (mode) {
    case 'single':
      parts = [normalized];
      break;
    case 'block':
      parts = normalized.split(/\n(?:[\t ]*\n)+/);
      break;
    default:
      parts = normalized.split('\n');
  }
  return parts.map((prompt) => prompt.trim()).filter(Boolean);
}

/** A prompt reduced for comparing: trimmed, runs of whitespace as one space. */
function promptKey(prompt: string): string {
  return prompt.trim().replace(/\s+/g, ' ');
}

/**
 * The prompts not waiting yet: repeats within the list and prompts already in
 * pending (the queue's unfinished prompts) are left out, so adding the same
 * text twice queues it once.
 */
export function newPrompts(prompts: readonly string[], pending: readonly string[]): string[] {
  const seen = new Set(pending.map(promptKey));
  const out: string[] = [];
  for (const prompt of prompts) {
    const key = promptKey(prompt);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push(prompt);
  }
  return out;
}
