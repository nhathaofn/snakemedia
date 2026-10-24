import { beforeEach, describe, expect, it } from 'vitest';
import { useFlowImageStore } from '../image/store/flowImageStore';
import { useFlowVideoStore } from '../video/store/flowVideoStore';
import type { FlowModel, FlowVideoModel, GeneratedImageItem } from '../../../types';

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
      prompt: '',
      referenceImages: [],
    });
  });

  it("selects Flow's default model when the catalogue loads", () => {
    useFlowImageStore.getState().setModels([imageModel('A'), imageModel('B', true)]);
    expect(useFlowImageStore.getState().selectedModel).toBe('B');
    useFlowImageStore.getState().setModels([imageModel('C')]);
    expect(useFlowImageStore.getState().selectedModel).toBe('C');
    useFlowImageStore.getState().setModels([]);
    expect(useFlowImageStore.getState().selectedModel).toBe('');
  });

  it('adds results newest first', () => {
    const item = (id: string) => ({ id }) as GeneratedImageItem;
    useFlowImageStore.getState().addItems([item('1')]);
    useFlowImageStore.getState().addItems([item('2'), item('3')]);
    expect(useFlowImageStore.getState().items.map((i) => i.id)).toEqual(['2', '3', '1']);
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
});

describe('video store', () => {
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
});
