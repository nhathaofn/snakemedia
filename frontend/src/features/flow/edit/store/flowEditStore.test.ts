import { beforeEach, describe, expect, it } from 'vitest';
import { clampTrim, trimRequest, useFlowEditStore } from './flowEditStore';
import { clockLabel } from '../../shared';
import { EditVideoRequest, UploadedSourceVideo } from '../../../../types';
import type { FlowEditModel, ImportVideoResult, GeneratedVideoItem } from '../../../../types';

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
      items: [],
      dismissedItemIds: [],
      source: null,
      isSourceLoading: false,
      sourceLoadingMessage: '',
      sourceError: null,
      uploadedSourceID: null,
      sourceDuration: 0,
      sourceWidth: 0,
      sourceHeight: 0,
      trimStart: 0,
      trimEnd: 0,
    });
  });

  it('tracks source loading, uploaded media id and error states', () => {
    const s = useFlowEditStore.getState();
    s.setSource(source);
    expect(useFlowEditStore.getState().isSourceLoading).toBe(true);
    expect(useFlowEditStore.getState().uploadedSourceID).toBeNull();
    expect(useFlowEditStore.getState().sourceError).toBeNull();

    // Player loads metadata first; still waiting for upload
    s.setSourceMetadata(10, 1920, 1080);
    expect(useFlowEditStore.getState().isSourceLoading).toBe(true);
    expect(useFlowEditStore.getState().sourceDuration).toBe(10);

    // Upload completes; both are ready
    s.setUploadedSource(
      UploadedSourceVideo.createFrom({
        minTrimSeconds: 0.5,
        trimStepSeconds: 0.1,
        sourceId: 'mid-abc-123',
        width: 1920,
        height: 1080,
        durationSeconds: 10,
        models: [omni],
      }),
      useFlowEditStore.getState().sourceRevision,
    );
    expect(useFlowEditStore.getState().isSourceLoading).toBe(false);
    expect(useFlowEditStore.getState().uploadedSourceID).toBe('mid-abc-123');

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

  it('sends the real range when the whole short video is used', () => {
    const s = useFlowEditStore.getState();
    s.setModels([omni]);
    s.setSource(source);
    useFlowEditStore.getState().setSourceMetadata(6, 720, 1280);
    expect(trimRequest(useFlowEditStore.getState())).toEqual({ trimStartMs: 0, trimEndMs: 6000 });
  });

  it('keeps the trim inside the video and the input limit', () => {
    expect(clampTrim(20, 40, 25, 10, 0.5)).toEqual({ trimStart: 20, trimEnd: 25 });
    expect(clampTrim(3, 30, 25, 10, 0.5)).toEqual({ trimStart: 3, trimEnd: 13 });
    expect(clampTrim(5, 5, 25, 10, 0.5)).toEqual({ trimStart: 5, trimEnd: 5.5 });
    expect(clampTrim(-1, 4, 0, 10, 0.5)).toEqual({ trimStart: 0, trimEnd: 0 });
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

it('ignores completion and errors from a previous source selection', () => {
  const s = useFlowEditStore.getState();
  const older = s.setSource(source);
  const newer = s.setSource({ ...source, localUrl: '/flow-media/new.mp4' } as ImportVideoResult);
  s.setUploadedSource(
    UploadedSourceVideo.createFrom({
      minTrimSeconds: 0.5,
      trimStepSeconds: 0.1,
      sourceId: 'new',
      width: 720,
      height: 1280,
      durationSeconds: 6,
      models: [omni],
    }),
    newer,
  );
  s.setUploadedSource(
    UploadedSourceVideo.createFrom({
      minTrimSeconds: 0.5,
      trimStepSeconds: 0.1,
      sourceId: 'old',
      width: 1280,
      height: 720,
      durationSeconds: 8,
      models: [omni],
    }),
    older,
  );
  s.setSourceError('stale error', older);
  s.setSourceMetadata(20, 1920, 1080, older);
  expect(useFlowEditStore.getState().uploadedSourceID).toBe('new');
  expect(useFlowEditStore.getState().sourceDuration).toBe(6);
  expect(useFlowEditStore.getState().sourceError).toBeNull();
});

it('preserves upload errors when metadata arrives late', () => {
  const s = useFlowEditStore.getState();
  const revision = s.setSource(source);
  s.setSourceError('upload failed', revision);
  s.setSourceMetadata(8, 1280, 720, revision);
  expect(useFlowEditStore.getState().sourceError).toBe('upload failed');
  expect(useFlowEditStore.getState().isSourceLoading).toBe(false);
});

it('keeps a completed result when stale pending history arrives later', () => {
  const s = useFlowEditStore.getState();
  useFlowEditStore.setState({ items: [] });
  s.upsertItems([{ id: 'pending', jobId: 'job', pending: true } as GeneratedVideoItem]);
  s.upsertItems([
    {
      id: 'output',
      jobId: 'job',
      pending: false,
      localUrl: '/flow-media/output.mp4',
    } as GeneratedVideoItem,
  ]);
  s.upsertItems([{ id: 'pending', jobId: 'job', pending: true } as GeneratedVideoItem]);
  expect(useFlowEditStore.getState().items).toHaveLength(1);
  expect(useFlowEditStore.getState().items[0].localUrl).toBe('/flow-media/output.mp4');
});

it('keeps a dismissed failed result hidden when the session updates items', () => {
  useFlowEditStore.setState({ items: [], dismissedItemIds: [] });
  const s = useFlowEditStore.getState();
  const failed = {
    id: 'failed-output',
    jobId: 'failed-job',
    error: 'Flow declined this edit',
    pending: false,
  } as GeneratedVideoItem;

  s.upsertItems([failed]);
  s.dismissItem(failed.jobId!);
  expect(useFlowEditStore.getState().items).toHaveLength(0);

  s.upsertItems([failed]);
  expect(useFlowEditStore.getState().items).toHaveLength(0);
  expect(useFlowEditStore.getState().dismissedItemIds).toContain(failed.jobId);
});

it('reuses the mutation ID after an uncertain response and preserves newer attempts', () => {
  const s = useFlowEditStore.getState();
  useFlowEditStore.setState({ editAttempt: null });
  const first = EditVideoRequest.createFrom({
    sourceId: 'source',
    modelId: 'model',
    prompt: 'first',
    trimStartMs: 0,
    trimEndMs: 1000,
  });
  const original = s.requestIDFor(first);
  expect(s.requestIDFor(first)).toBe(original);
  const changed = EditVideoRequest.createFrom({ ...first, prompt: 'changed' });
  const next = s.requestIDFor(changed);
  expect(next).not.toBe(original);
  s.settleAttempt(original);
  expect(s.requestIDFor(changed)).toBe(next);
  s.settleAttempt(next);
  expect(s.requestIDFor(changed)).not.toBe(next);
});
