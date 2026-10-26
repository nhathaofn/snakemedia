import { beforeEach, describe, expect, it } from 'vitest';
import { FlowImageQuality } from '../../../../types';
import type { GeneratedImageItem } from '../../../../types';
import { useFlowImageStore } from './flowImageStore';

describe('updateAllPrompt', () => {
  beforeEach(() => {
    useFlowImageStore.setState({ allPrompts: [] });
    useFlowImageStore
      .getState()
      .addAllPrompts(['a fox'], 'm', '16:9', FlowImageQuality.OneK, 1, []);
  });

  const task = () => useFlowImageStore.getState().allPrompts[0];

  it('keeps an uncertain request ID across status changes', () => {
    useFlowImageStore.getState().updateAllPrompt(task().id, { requestId: 'req-1' });
    useFlowImageStore.getState().updateAllPrompt(task().id, { status: 'queued' });
    expect(task().requestId).toBe('req-1');
  });

  it('drops the request ID when the references change the request', () => {
    useFlowImageStore.getState().updateAllPrompt(task().id, { requestId: 'req-1' });
    useFlowImageStore.getState().updateAllPrompt(task().id, {
      references: [{ base64: 'x', mimeType: 'image/png', name: 'r' }],
    });
    expect(task().requestId).toBeUndefined();
  });

  it('restores saved All jobs once and makes them ordinary tasks when edited', () => {
    const job = {
      id: 'c1',
      jobId: 'job-1',
      pending: true,
      prompt: 'a fox',
      model: 'm',
      aspectRatio: '16:9',
    } as GeneratedImageItem;
    const store = useFlowImageStore.getState();
    store.restoreAllPrompts([job], FlowImageQuality.OneK);
    useFlowImageStore.getState().restoreAllPrompts([job], FlowImageQuality.OneK);
    const restored = useFlowImageStore.getState().allPrompts.filter((t) => t.restored);
    expect(restored).toHaveLength(1);
    expect(restored[0]).toMatchObject({ requestId: 'job-1', status: 'failed', prompt: 'a fox' });

    useFlowImageStore.getState().updateAllPrompt(restored[0].id, {
      references: [{ base64: 'x', mimeType: 'image/png', name: 'r' }],
    });
    const edited = useFlowImageStore.getState().allPrompts.find((t) => t.id === restored[0].id);
    expect(edited).toMatchObject({ restored: false, requestId: undefined });
  });
});
