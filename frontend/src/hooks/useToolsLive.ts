import { useEffect } from 'react';
import { useToolsStore } from '../stores';
import { AppEvent } from '../types';
import type { ToolProgress, ToolRequest } from '../types';
import { useWailsEvent } from './useWailsEvent';

/** Loads the tool list and unanswered asks once, and keeps them current. */
export function useToolsLive() {
  const load = useToolsStore((s) => s.load);
  const ask = useToolsStore((s) => s.ask);
  const setProgress = useToolsStore((s) => s.setProgress);

  useEffect(() => {
    void load();
  }, [load]);

  useWailsEvent<ToolRequest>(AppEvent.ToolMissing, ask);
  useWailsEvent<ToolProgress>(AppEvent.ToolProgress, setProgress);
}
