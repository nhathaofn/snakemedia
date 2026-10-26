import { beforeEach, describe, expect, it } from 'vitest';
import { useFlowImageStore } from '../image/store/flowImageStore';
import { useFlowVideoStore } from '../video/store/flowVideoStore';
import type {
  FlowModel,
  FlowVideoModel,
  GeneratedImageItem,
  GeneratedVideoItem,
} from '../../../types';

const imageModel = (id: string, isDefault = false) =>
  ({ id, name: id, default: isDefault, description: '', maxReferenceImages: 10 }) as FlowModel;

const videoModel = (id: string, durations: number[], ratios: string[], resolutions: string[]) =>
  ({
    id,
    name: id,
    default: false,
    durations,
    aspectRatios: ratios,
    resolutions,
  }) as FlowVideoModel;

describe('shared studio slice', () => {
  beforeEach(() => {
    useFlowImageStore.setState({
      models: [],
      selectedModel: '',
      items: [],
      turns: [],
      prompt: '',
      referenceImages: [],
    });
  });

  it("selects Flow's default model when the catalogue loads", () => {
    useFlowImageStore.getState().setModels([imageModel('A'), imageModel('B', true)]);
    expect(useFlowImageStore.getState().selectedModel).toBe('B');
    useFlowImageStore.getState().setModels([imageModel('C')]);
    expect(useFlowImageStore.getState().selectedModel).toBe('C');
  });

  it('keeps the chosen model when the same catalogue loads again', () => {
    const store = useFlowImageStore.getState();
    store.setModels([imageModel('A'), imageModel('B', true)]);
    useFlowImageStore.getState().setSelectedModel('A');
    useFlowImageStore.getState().setModels([imageModel('A'), imageModel('B', true)]);
    expect(useFlowImageStore.getState().selectedModel).toBe('A');
    useFlowImageStore.getState().setModels([]);
    expect(useFlowImageStore.getState().selectedModel).toBe('');
  });

  it('adds results newest first', () => {
    const item = (id: string) => ({ id }) as GeneratedImageItem;
    useFlowImageStore.getState().addItems([item('1')]);
    useFlowImageStore.getState().addItems([item('2'), item('3')]);
    expect(useFlowImageStore.getState().items.map((i) => i.id)).toEqual(['2', '3', '1']);
  });

  it('keeps the busy state until every concurrent request finishes', () => {
    const store = useFlowImageStore.getState();
    store.beginGeneration();
    store.beginGeneration();
    expect(useFlowImageStore.getState()).toMatchObject({
      activeGenerations: 2,
      isGenerating: true,
    });

    useFlowImageStore.getState().finishGeneration();
    expect(useFlowImageStore.getState()).toMatchObject({
      activeGenerations: 1,
      isGenerating: true,
    });

    useFlowImageStore.getState().finishGeneration();
    expect(useFlowImageStore.getState()).toMatchObject({
      activeGenerations: 0,
      isGenerating: false,
    });
  });

  it('keeps each prompt and its result keys together in submission order', () => {
    const store = useFlowImageStore.getState();
    const first = {
      id: 'turn-1',
      prompt: 'Một ngôi nhà bên hồ',
      attachments: [
        { id: 'blob:reference', kind: 'image' as const, name: 'ref.png', url: 'blob:reference' },
      ],
      pendingCount: 2,
      pendingRatio: '16:9',
      pendingLabel: 'Đang tạo ảnh',
    };
    store.beginTurn(first);
    store.completeTurn('turn-1', [{ id: 'image-1' }, { id: 'image-2' }] as GeneratedImageItem[]);
    useFlowImageStore.getState().beginTurn({ ...first, id: 'turn-2', prompt: 'Ánh sáng buổi sớm' });

    expect(useFlowImageStore.getState().turns).toMatchObject([
      { id: 'turn-1', prompt: first.prompt, resultKeys: ['image-1', 'image-2'], pendingCount: 0 },
      { id: 'turn-2', prompt: 'Ánh sáng buổi sớm', resultKeys: [], pendingCount: 2 },
    ]);
  });

  it('keeps reference images until removed', () => {
    const ref = (n: number) => ({
      base64: 'x',
      name: `${n}.png`,
      mimeType: 'image/png',
      previewUrl: `blob:${n}`,
    });
    useFlowImageStore.getState().addReferenceImages([ref(1), ref(2)]);
    useFlowImageStore.getState().removeReferenceImage('blob:1');
    expect(useFlowImageStore.getState().referenceImages.map((r) => r.name)).toEqual(['2.png']);
  });

  it('shows a failed concurrent turn in the conversation', () => {
    const store = useFlowImageStore.getState();
    store.beginTurn({
      id: 'failed-turn',
      prompt: 'Một bông hoa dưới mưa',
      attachments: [],
      pendingCount: 1,
      pendingRatio: '1:1',
      pendingLabel: 'Đang tạo ảnh',
    });
    useFlowImageStore.getState().failTurn('failed-turn', 'Flow đang bận.');

    expect(useFlowImageStore.getState().turns[0]).toMatchObject({
      id: 'failed-turn',
      prompt: 'Một bông hoa dưới mưa',
      pendingCount: 0,
      error: 'Flow đang bận.',
    });
  });
});

describe('video store', () => {
  it('keeps a video job linked when its item id changes during generation', () => {
    useFlowVideoStore.setState({ turns: [] });
    const store = useFlowVideoStore.getState();
    store.beginTurn({
      id: 'video-turn',
      prompt: 'Một cảnh biển lúc hoàng hôn',
      attachments: [],
      pendingCount: 1,
      pendingRatio: '16:9',
      pendingLabel: 'Đang tạo video',
    });
    store.completeTurn('video-turn', [
      { id: 'temporary-result-id', jobId: 'flow-job' },
    ] as GeneratedVideoItem[]);
    expect(useFlowVideoStore.getState().turns[0].resultKeys).toEqual([
      'flow-job:temporary-result-id',
    ]);
  });

  it('fits ratio, duration and quality to the selected model', () => {
    const omni = videoModel('abra', [4, 6, 8, 10], ['16:9', '9:16'], ['720p', '360p']);
    const veo = videoModel('veo', [8], ['16:9'], ['720p']);
    const store = useFlowVideoStore.getState();
    store.setModels([omni, veo]);
    expect(useFlowVideoStore.getState()).toMatchObject({
      selectedModel: 'abra',
      aspectRatio: '16:9',
      duration: 10,
      resolution: '720p',
    });

    useFlowVideoStore.getState().setAspectRatio('9:16');
    useFlowVideoStore.getState().setDuration(4);
    useFlowVideoStore.getState().setResolution('360p');
    useFlowVideoStore.getState().setSelectedModel('veo');
    expect(useFlowVideoStore.getState()).toMatchObject({
      aspectRatio: '16:9',
      duration: 8,
      resolution: '720p',
    });

    useFlowVideoStore.getState().setSelectedModel('abra');
    expect(useFlowVideoStore.getState()).toMatchObject({
      aspectRatio: '16:9',
      duration: 8,
      resolution: '720p',
    });
  });

  it('restores saved jobs once, before the turns of this session', () => {
    useFlowImageStore.setState({ turns: [], items: [] });
    const pending = {
      id: 'c1',
      jobId: 'job-1',
      pending: true,
      prompt: 'a fox',
    } as GeneratedImageItem;
    const store = useFlowImageStore.getState();
    store.beginTurn({
      id: 'job-2',
      prompt: 'new',
      attachments: [],
      pendingCount: 1,
      pendingRatio: '1:1',
      pendingLabel: '',
    });
    const entry = {
      turn: {
        id: 'job-1',
        prompt: 'a fox',
        attachments: [],
        pendingCount: 0,
        pendingRatio: '1:1',
        pendingLabel: '',
      },
      items: [pending],
    };
    useFlowImageStore.getState().restoreTurns([entry]);
    useFlowImageStore.getState().restoreTurns([entry]);
    const { turns, items } = useFlowImageStore.getState();
    expect(turns.map((turn) => turn.id)).toEqual(['job-1', 'job-2']);
    expect(turns[0].resultKeys).toEqual(['job-1:c1']);
    expect(items).toHaveLength(1);
  });

  it('replaces a pending placeholder with its finished result', () => {
    useFlowImageStore.setState({ turns: [], items: [] });
    const pending = { id: 'c1', jobId: 'job-1', pending: true } as GeneratedImageItem;
    const done = {
      id: 'media-1',
      jobId: 'job-1',
      localUrl: '/flow-media/1.png',
    } as GeneratedImageItem;
    const store = useFlowImageStore.getState();
    store.beginTurn({
      id: 'job-1',
      prompt: 'p',
      attachments: [],
      pendingCount: 1,
      pendingRatio: '',
      pendingLabel: '',
    });
    store.addItems([pending]);
    useFlowImageStore.getState().completeTurn('job-1', [pending]);
    useFlowImageStore.getState().addItems([done]);
    useFlowImageStore.getState().completeTurn('job-1', [done]);
    const { turns, items } = useFlowImageStore.getState();
    expect(turns[0].resultKeys).toEqual(['job-1:media-1']);
    expect(items.map((item) => item.id)).toEqual(['media-1']);
  });

  it('resets a turn when the same request is sent again', () => {
    useFlowImageStore.setState({ turns: [], items: [] });
    const store = useFlowImageStore.getState();
    const draft = {
      id: 'job-1',
      prompt: 'p',
      attachments: [],
      pendingCount: 1,
      pendingRatio: '',
      pendingLabel: '',
    };
    store.beginTurn(draft);
    useFlowImageStore.getState().failTurn('job-1', 'mất kết nối');
    useFlowImageStore.getState().beginTurn(draft);
    const { turns } = useFlowImageStore.getState();
    expect(turns).toHaveLength(1);
    expect(turns[0]).toMatchObject({ pendingCount: 1 });
    expect(turns[0].error).toBeUndefined();
  });
});
