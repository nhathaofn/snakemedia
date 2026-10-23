import { beforeEach, describe, expect, it } from 'vitest';
import { useFlowImageStore } from './flowImageStore';
import type { GeneratedImageItem } from './flowImageStore';

describe('useFlowImageStore', () => {
  beforeEach(() => {
    useFlowImageStore.setState({
      selectedModel: '',
      aspectRatio: '1:1',
      quantity: 1,
      prompt: '',
      isGenerating: false,
      images: [],
      previewImage: null,
    });
  });

  it('updates selected model', () => {
    useFlowImageStore.getState().setSelectedModel('NARWHAL');
    expect(useFlowImageStore.getState().selectedModel).toBe('NARWHAL');
  });

  it('updates aspect ratio and quantity', () => {
    useFlowImageStore.getState().setAspectRatio('16:9');
    useFlowImageStore.getState().setQuantity(4);
    expect(useFlowImageStore.getState().aspectRatio).toBe('16:9');
    expect(useFlowImageStore.getState().quantity).toBe(4);
  });

  it('updates prompt and generating status', () => {
    useFlowImageStore.getState().setPrompt('A futuristic city');
    useFlowImageStore.getState().setIsGenerating(true);
    expect(useFlowImageStore.getState().prompt).toBe('A futuristic city');
    expect(useFlowImageStore.getState().isGenerating).toBe(true);
  });

  it('prepends newly added images', () => {
    const item1: GeneratedImageItem = {
      id: 'img-1',
      url: 'https://example.com/1.png',
      prompt: 'Prompt 1',
      model: 'Nano 2',
      aspectRatio: '1:1',
      createdAt: '2026-10-23T14:00:00Z',
    };
    const item2: GeneratedImageItem = {
      id: 'img-2',
      url: 'https://example.com/2.png',
      prompt: 'Prompt 2',
      model: 'Nano Pro',
      aspectRatio: '16:9',
      createdAt: '2026-10-23T14:01:00Z',
    };

    useFlowImageStore.getState().addImages([item1]);
    expect(useFlowImageStore.getState().images).toHaveLength(1);

    useFlowImageStore.getState().addImages([item2]);
    expect(useFlowImageStore.getState().images).toHaveLength(2);
    expect(useFlowImageStore.getState().images[0].id).toBe('img-2');
  });

  it('manages preview image lightbox state', () => {
    const item: GeneratedImageItem = {
      id: 'img-1',
      url: 'https://example.com/1.png',
      prompt: 'Prompt 1',
      model: 'Nano 2',
      aspectRatio: '1:1',
      createdAt: '2026-10-23T14:00:00Z',
    };

    useFlowImageStore.getState().setPreviewImage(item);
    expect(useFlowImageStore.getState().previewImage).toBe(item);

    useFlowImageStore.getState().setPreviewImage(null);
    expect(useFlowImageStore.getState().previewImage).toBeNull();
  });
});
