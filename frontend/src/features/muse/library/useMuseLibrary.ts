import { useCallback, useEffect, useRef, useState } from 'react';
import { onNextFrame, useWailsEvent } from '../../../hooks';
import { api, unwrapList } from '../../../services';
import { AppEvent, MuseJobStatus } from '../../../types';
import type { MuseJob, MuseLibraryItem } from '../../../types';

async function fetchLibrary(): Promise<MuseLibraryItem[]> {
  return unwrapList(await api.listMuseLibrary());
}

/**
 * The Muse library: loaded on mount and again whenever a job succeeds, since
 * that is the only time the backend adds files to it.
 */
export function useMuseLibrary() {
  const [items, setItems] = useState<MuseLibraryItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const apply = useCallback((load: Promise<MuseLibraryItem[]>) => {
    return load
      .then((next) => {
        setItems(next);
        setError(null);
      })
      .catch((cause: unknown) => setError(cause instanceof Error ? cause.message : String(cause)))
      .finally(() => setLoading(false));
  }, []);

  const reload = useCallback(() => apply(fetchLibrary()), [apply]);

  useEffect(() => {
    void apply(fetchLibrary());
  }, [apply]);

  // Jobs finishing in a burst share one reload.
  const reloadQueued = useRef(false);
  useWailsEvent<MuseJob | null>(AppEvent.MuseJobUpdated, (job) => {
    if (job?.status !== MuseJobStatus.Succeeded || reloadQueued.current) return;
    reloadQueued.current = true;
    onNextFrame(() => {
      reloadQueued.current = false;
      void reload();
    });
  });

  return { items, loading, error, reload };
}
