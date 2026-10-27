import { beforeEach, describe, expect, it } from 'vitest';
import type { FlowImageTaskReference } from '../../../../types';
import { useFlowImageStore } from './flowImageStore';

const ref = (localUrl: string) => ({ localUrl, fileName: localUrl }) as FlowImageTaskReference;

describe('queue references', () => {
  beforeEach(() => useFlowImageStore.setState({ queueReferences: [] }));

  it('adds each media file once and removes by URL', () => {
    const store = useFlowImageStore.getState();
    store.addQueueReferences([ref('/flow-media/a.png'), ref('/flow-media/b.png')]);
    useFlowImageStore.getState().addQueueReferences([ref('/flow-media/a.png')]);
    expect(useFlowImageStore.getState().queueReferences.map((r) => r.localUrl)).toEqual([
      '/flow-media/a.png',
      '/flow-media/b.png',
    ]);
    useFlowImageStore.getState().removeQueueReference('/flow-media/a.png');
    expect(useFlowImageStore.getState().queueReferences.map((r) => r.localUrl)).toEqual([
      '/flow-media/b.png',
    ]);
  });
});
