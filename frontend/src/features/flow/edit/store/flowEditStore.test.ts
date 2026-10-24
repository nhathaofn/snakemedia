import { beforeEach, describe, expect, it } from 'vitest';
import { clampTrim, trimRequest, useFlowEditStore } from './flowEditStore';
import { clockLabel } from '../../shared';
import type { FlowEditModel, ImportVideoResult } from '../../../../types';

const omni = {
  id: 'abra',
  name: 'Omni 1.1 Flash',
  default: true,
  duration: 8,
  maxInputSeconds: 10,
} as FlowEditModel;

const source = {
  localUrl: '/flow-media/source-1.mp4',
  fileName: 'a.mp4',
  size: 1,
} as ImportVideoResult;

describe('edit video store', () => {
  beforeEach(() => {
    useFlowEditStore.setState({
      models: [],
      selectedModel: '',
      source: null,
      isSourceLoading: false,
      sourceLoadingMessage: '',
      sourceError: null,
      uploadedMediaId: null,
      sourceDuration: 0,
      trimStart: 0,
      trimEnd: 0,
    });
  });

  it('tracks source loading, uploaded media id and error states', () => {
    const s = useFlowEditStore.getState();
    s.setSource(source);
    expect(useFlowEditStore.getState().isSourceLoading).toBe(true);
    expect(useFlowEditStore.getState().uploadedMediaId).toBeNull();
    expect(useFlowEditStore.getState().sourceError).toBeNull();

    // Player loads metadata first; still waiting for upload
    s.setSourceMetadata(10, 1920, 1080);
    expect(useFlowEditStore.getState().isSourceLoading).toBe(true);
    expect(useFlowEditStore.getState().sourceDuration).toBe(10);

    // Upload completes; both are ready
    s.setUploadedMedia('mid-abc-123', 1920, 1080);
    expect(useFlowEditStore.getState().isSourceLoading).toBe(false);
    expect(useFlowEditStore.getState().uploadedMediaId).toBe('mid-abc-123');

    s.setSourceError('Failed to load video');
    expect(useFlowEditStore.getState().isSourceLoading).toBe(false);
    expect(useFlowEditStore.getState().sourceError).toBe('Failed to load video');
  });

  it('starts a long video with the longest part the model reads', () => {
    const s = useFlowEditStore.getState();
    s.setModels([omni]);
    s.setSource(source);
    useFlowEditStore.getState().setSourceMetadata(25, 1280, 720);
    const { trimStart, trimEnd, sourceWidth } = useFlowEditStore.getState();
    expect([trimStart, trimEnd]).toEqual([0, 10]);
    expect(sourceWidth).toBe(1280);
    expect(trimRequest(useFlowEditStore.getState())).toEqual({ trimStartMs: 0, trimEndMs: 10000 });
  });

  it('sends no trim when the whole short video is used', () => {
    const s = useFlowEditStore.getState();
    s.setModels([omni]);
    s.setSource(source);
    useFlowEditStore.getState().setSourceMetadata(6, 720, 1280);
    expect(trimRequest(useFlowEditStore.getState())).toEqual({ trimStartMs: 0, trimEndMs: 0 });
  });

  it('keeps the trim inside the video and the input limit', () => {
    expect(clampTrim(20, 40, 25, 10)).toEqual({ trimStart: 20, trimEnd: 25 });
    expect(clampTrim(3, 30, 25, 10)).toEqual({ trimStart: 3, trimEnd: 13 });
    expect(clampTrim(5, 5, 25, 10)).toEqual({ trimStart: 5, trimEnd: 5.5 });
    expect(clampTrim(-1, 4, 0, 10)).toEqual({ trimStart: 0, trimEnd: 0 });
  });

  it('never rounds a trim past the limit', () => {
    const req = trimRequest({ sourceDuration: 30, trimStart: 1.0004, trimEnd: 11.0004 });
    expect(req.trimEndMs - req.trimStartMs).toBeLessThanOrEqual(10000);
  });

  it('labels video positions', () => {
    expect(clockLabel(8.46)).toBe('0:08.5');
    expect(clockLabel(75)).toBe('1:15.0');
  });
});
