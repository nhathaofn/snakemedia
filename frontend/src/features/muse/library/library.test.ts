import { describe, expect, it } from 'vitest';
import { MuseJobKind } from '../../../types';
import type { MuseLibraryItem } from '../../../types';
import { filterLibrary, libraryTitle } from './library';

const item = (id: string, kind: MuseJobKind, label = ''): MuseLibraryItem =>
  ({
    id,
    jobId: 'j',
    kind,
    localUrl: `/flow-media/${id}`,
    prompt: `p-${id}`,
    label,
    createdAt: '',
  }) as MuseLibraryItem;

describe('muse library', () => {
  const items = [
    item('a', MuseJobKind.Image),
    item('b', MuseJobKind.Video),
    item('c', MuseJobKind.Image),
  ];

  it('keeps the backend order and filters by kind', () => {
    expect(filterLibrary(items, 'all').map((i) => i.id)).toEqual(['a', 'b', 'c']);
    expect(filterLibrary(items, MuseJobKind.Image).map((i) => i.id)).toEqual(['a', 'c']);
    expect(filterLibrary(items, MuseJobKind.Video).map((i) => i.id)).toEqual(['b']);
  });

  it('titles a tile by label, else prompt', () => {
    expect(libraryTitle(item('a', MuseJobKind.Image, 'Mèo'))).toBe('Mèo');
    expect(libraryTitle(item('b', MuseJobKind.Image))).toBe('p-b');
  });
});
