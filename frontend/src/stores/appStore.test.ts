import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DEFAULT_TOAST_DURATION_MS } from '../constants';
import { useAppStore } from './appStore';

beforeEach(() => {
  vi.useFakeTimers();
  useAppStore.setState({ toasts: [] });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('toasts', () => {
  it('auto-dismisses after the default duration', () => {
    useAppStore.getState().addToast({ type: 'info', message: 'hi' });
    expect(useAppStore.getState().toasts).toHaveLength(1);

    vi.advanceTimersByTime(DEFAULT_TOAST_DURATION_MS);
    expect(useAppStore.getState().toasts).toHaveLength(0);
  });

  it('keeps sticky toasts until removed', () => {
    const id = useAppStore.getState().addToast({ type: 'error', message: 'stay', durationMs: 0 });
    vi.advanceTimersByTime(DEFAULT_TOAST_DURATION_MS * 10);
    expect(useAppStore.getState().toasts).toHaveLength(1);

    useAppStore.getState().removeToast(id);
    expect(useAppStore.getState().toasts).toHaveLength(0);
  });
});
