import type { MuseJobKind, MuseLibraryItem } from '../../../types';

export type KindFilter = 'all' | MuseJobKind;
export type LibrarySort = 'newest' | 'oldest' | 'title';

/** Library items of the chosen kind, in the order the backend lists them (newest first). */
export function filterLibrary(
  items: readonly MuseLibraryItem[],
  filter: KindFilter,
): MuseLibraryItem[] {
  return filter === 'all' ? [...items] : items.filter((item) => item.kind === filter);
}

/** Filters library items by kind + search text and applies the selected sort order. */
export function filterAndSortLibrary(
  items: readonly MuseLibraryItem[],
  filter: KindFilter,
  query = '',
  sort: LibrarySort = 'newest',
): MuseLibraryItem[] {
  const byKind = filterLibrary(items, filter);
  const q = query.trim().toLowerCase();
  const filtered = q
    ? byKind.filter(
        (item) =>
          item.prompt.toLowerCase().includes(q) || (item.label ?? '').toLowerCase().includes(q),
      )
    : byKind;

  if (sort === 'oldest') {
    return [...filtered].reverse();
  }
  if (sort === 'title') {
    return [...filtered].sort((a, b) => libraryTitle(a).localeCompare(libraryTitle(b), 'vi'));
  }
  return filtered;
}

/** Formats an ISO timestamp concisely for library cards. */
export function formatLibraryDate(iso: string): string {
  if (!iso) return '';
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(date.getDate())}/${pad(date.getMonth() + 1)} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** The title a library tile shows: the job's label, else its prompt. */
export function libraryTitle(item: MuseLibraryItem): string {
  return item.label || item.prompt;
}
