import type { MuseJobKind, MuseLibraryItem } from '../../../types';

export type KindFilter = 'all' | MuseJobKind;

/** Library items of the chosen kind, in the order the backend lists them (newest first). */
export function filterLibrary(
  items: readonly MuseLibraryItem[],
  filter: KindFilter,
): MuseLibraryItem[] {
  return filter === 'all' ? [...items] : items.filter((item) => item.kind === filter);
}

/** The title a library tile shows: the job's label, else its prompt. */
export function libraryTitle(item: MuseLibraryItem): string {
  return item.label || item.prompt;
}
