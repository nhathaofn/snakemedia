import { useEffect, useRef } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { isWailsRuntime } from '../services';
import type { AppEvent } from '../types';

/**
 * Subscribes to a backend event for the lifetime of the component.
 * Only this listener is removed on unmount; other subscribers are unaffected.
 */
export function useWailsEvent<T = unknown>(event: AppEvent, handler: (data: T) => void): void {
  const handlerRef = useRef(handler);

  useEffect(() => {
    handlerRef.current = handler;
  }, [handler]);

  useEffect(() => {
    if (!isWailsRuntime()) return;
    return EventsOn(event, (data: T) => handlerRef.current(data));
  }, [event]);
}
