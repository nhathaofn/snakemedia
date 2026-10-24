import { describe, expect, it, vi } from 'vitest';
import { create } from 'zustand';
import { runGeneration } from './hooks';
import { ResponseCode, type ApiResponse } from '../../../types';

interface Item {
  id: string;
  error?: string;
}

function makeStore(prompt: string) {
  return create<{
    prompt: string;
    setPrompt: (prompt: string) => void;
    isGenerating: boolean;
    setIsGenerating: (generating: boolean) => void;
    items: Item[];
    addItems: (items: Item[]) => void;
  }>((set) => ({
    prompt,
    setPrompt: (next) => set({ prompt: next }),
    isGenerating: false,
    setIsGenerating: (isGenerating) => set({ isGenerating }),
    items: [],
    addItems: (items) => set((s) => ({ items: [...items, ...s.items] })),
  }));
}

const ok = (data: Item[]): ApiResponse<Item[]> => ({ success: true, data, code: ResponseCode.OK });
const fail: ApiResponse<Item[]> = { success: false, error: 'boom', code: ResponseCode.Internal };

describe('runGeneration', () => {
  it('clears the prompt as soon as it is sent and keeps it cleared on success', async () => {
    const store = makeStore('a cat');
    const notify = vi.fn();
    let promptWhileWaiting = '';
    await runGeneration(
      store,
      async () => {
        promptWhileWaiting = store.getState().prompt;
        expect(store.getState().isGenerating).toBe(true);
        return ok([{ id: '1' }]);
      },
      'ảnh',
      notify,
    );
    expect(promptWhileWaiting).toBe('');
    expect(store.getState().prompt).toBe('');
    expect(store.getState().items).toHaveLength(1);
    expect(store.getState().isGenerating).toBe(false);
    expect(notify).not.toHaveBeenCalled();
  });

  it('gives the prompt back when generation fails', async () => {
    const store = makeStore('a cat');
    const notify = vi.fn();
    await runGeneration(store, async () => fail, 'ảnh', notify);
    expect(store.getState().prompt).toBe('a cat');
    expect(notify).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'error', message: 'boom' }),
    );
  });

  it('gives the prompt back when the request throws', async () => {
    const store = makeStore('a cat');
    await expect(
      runGeneration(store, () => Promise.reject(new Error('down')), 'ảnh', vi.fn()),
    ).rejects.toThrow('down');
    expect(store.getState().prompt).toBe('a cat');
    expect(store.getState().isGenerating).toBe(false);
  });

  it('keeps what the user typed meanwhile instead of restoring', async () => {
    const store = makeStore('a cat');
    await runGeneration(
      store,
      async () => {
        store.getState().setPrompt('a dog');
        return fail;
      },
      'ảnh',
      vi.fn(),
    );
    expect(store.getState().prompt).toBe('a dog');
  });

  it('ignores a second submit while one is running', async () => {
    const store = makeStore('a cat');
    store.getState().setIsGenerating(true);
    const request = vi.fn(async () => ok([]));
    await runGeneration(store, request, 'ảnh', vi.fn());
    expect(request).not.toHaveBeenCalled();
    expect(store.getState().prompt).toBe('a cat');
  });
});
