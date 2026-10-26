import { afterEach, describe, expect, it, vi } from 'vitest';
import { create } from 'zustand';
import { beginGenerationAttempt, clearGenerationAttempt, runGeneration } from './hooks';
import { invoke } from '../../../services/api';
import { ResponseCode, type ApiResponse } from '../../../types';

afterEach(() => {
  vi.unstubAllGlobals();
});

interface Item {
  id: string;
  error?: string;
}

function makeStore(prompt: string) {
  return create<{
    prompt: string;
    setPrompt: (prompt: string) => void;
    isGenerating: boolean;
    activeGenerations: number;
    beginGeneration: () => void;
    finishGeneration: () => void;
    items: Item[];
    addItems: (items: Item[]) => void;
  }>((set) => ({
    prompt,
    setPrompt: (next) => set({ prompt: next }),
    isGenerating: false,
    activeGenerations: 0,
    beginGeneration: () =>
      set((state) => ({ activeGenerations: state.activeGenerations + 1, isGenerating: true })),
    finishGeneration: () =>
      set((state) => {
        const activeGenerations = Math.max(0, state.activeGenerations - 1);
        return { activeGenerations, isGenerating: activeGenerations > 0 };
      }),
    items: [],
    addItems: (items) => set((s) => ({ items: [...items, ...s.items] })),
  }));
}

const ok = (data: Item[]): ApiResponse<Item[]> => ({ success: true, data, code: ResponseCode.OK });
const fail: ApiResponse<Item[]> = { success: false, error: 'boom', code: ResponseCode.Internal };

describe('runGeneration', () => {
  it('reuses the request ID while a matching Flow request is pending', async () => {
    const request = { prompt: 'a cat', modelId: 'BELUGA' };
    const first = await beginGenerationAttempt('image', request);
    first.settle(true);
    const retry = await beginGenerationAttempt('image', request);
    expect(retry.requestId).toBe(first.requestId);
    clearGenerationAttempt(retry.requestId);
    const next = await beginGenerationAttempt('image', request);
    expect(next.requestId).not.toBe(first.requestId);
    clearGenerationAttempt(next.requestId);
  });

  it('gives a deliberate resend of an in-flight prompt its own request ID', async () => {
    const request = { prompt: 'a fox', modelId: 'BELUGA' };
    const first = await beginGenerationAttempt('image', request);
    const resend = await beginGenerationAttempt('image', request);
    expect(resend.requestId).not.toBe(first.requestId);
    resend.settle(false);
    first.settle(true);
    // Once the uncertain attempt is no longer in flight, a retry reconciles it.
    const retry = await beginGenerationAttempt('image', request);
    expect(retry.requestId).toBe(first.requestId);
    retry.settle(false);
  });

  it('treats a bridge failure as uncertain, not as a Flow rejection', async () => {
    vi.stubGlobal('window', { go: {}, runtime: {} });
    const store = makeStore('a cat');
    const lifecycle = { onFailure: vi.fn(), onUncertainFailure: vi.fn() };
    await runGeneration(
      store,
      () => invoke<Item[]>('GenerateImages', () => Promise.reject(new Error('ipc closed'))),
      'ảnh',
      vi.fn(),
      lifecycle,
    );
    expect(lifecycle.onFailure).not.toHaveBeenCalled();
    expect(lifecycle.onUncertainFailure).toHaveBeenCalledWith('GenerateImages: ipc closed', true);

    await runGeneration(store, async () => fail, 'ảnh', vi.fn(), lifecycle);
    expect(lifecycle.onFailure).toHaveBeenCalledWith('boom', true);
  });

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

  it('accepts another prompt while a request is running and tracks both', async () => {
    const store = makeStore('a cat');
    let finishFirst!: (response: ApiResponse<Item[]>) => void;
    let finishSecond!: (response: ApiResponse<Item[]>) => void;
    const firstRequest = vi.fn(
      () => new Promise<ApiResponse<Item[]>>((resolve) => (finishFirst = resolve)),
    );
    const secondRequest = vi.fn(
      () => new Promise<ApiResponse<Item[]>>((resolve) => (finishSecond = resolve)),
    );

    const first = runGeneration(store, firstRequest, 'ảnh', vi.fn());
    expect(store.getState()).toMatchObject({
      prompt: '',
      activeGenerations: 1,
      isGenerating: true,
    });

    store.getState().setPrompt('a dog');
    const second = runGeneration(store, secondRequest, 'ảnh', vi.fn());
    expect(firstRequest).toHaveBeenCalledOnce();
    expect(secondRequest).toHaveBeenCalledOnce();
    expect(store.getState()).toMatchObject({
      prompt: '',
      activeGenerations: 2,
      isGenerating: true,
    });

    finishFirst(ok([{ id: '1' }]));
    await first;
    expect(store.getState()).toMatchObject({ activeGenerations: 1, isGenerating: true });

    finishSecond(ok([{ id: '2' }]));
    await second;
    expect(store.getState()).toMatchObject({ activeGenerations: 0, isGenerating: false });
    expect(store.getState().items.map((item) => item.id)).toEqual(['2', '1']);
  });

  it('restores only the last concurrent failure and reports earlier ones on their turns', async () => {
    const store = makeStore('first prompt');
    let failFirst!: (response: ApiResponse<Item[]>) => void;
    let failSecond!: (response: ApiResponse<Item[]>) => void;
    const firstLifecycle = { onFailure: vi.fn() };
    const secondLifecycle = { onFailure: vi.fn() };

    const first = runGeneration(
      store,
      () => new Promise<ApiResponse<Item[]>>((resolve) => (failFirst = resolve)),
      'ảnh',
      vi.fn(),
      firstLifecycle,
    );
    store.getState().setPrompt('second prompt');
    const second = runGeneration(
      store,
      () => new Promise<ApiResponse<Item[]>>((resolve) => (failSecond = resolve)),
      'ảnh',
      vi.fn(),
      secondLifecycle,
    );

    failFirst(fail);
    await first;
    expect(firstLifecycle.onFailure).toHaveBeenCalledWith('boom', false);
    expect(store.getState().prompt).toBe('');

    failSecond(fail);
    await second;
    expect(secondLifecycle.onFailure).toHaveBeenCalledWith('boom', true);
    expect(store.getState().prompt).toBe('second prompt');
  });
});
