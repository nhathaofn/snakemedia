/** Split a prompt list at blank lines while preserving multiline prompt text. */
export function parsePromptList(text: string): string[] {
  return text
    .replace(/^\uFEFF/, '')
    .replace(/\r\n?/g, '\n')
    .split(/\n[\t ]*\n+/)
    .map((prompt) => prompt.trim())
    .filter(Boolean);
}
