import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ResponseCode } from '../../../types';
import type * as Services from '../../../services';
import type { MuseJob } from '../../../types';

const backend = vi.hoisted(() => ({
  listMuseJobs: vi.fn(),
  getMuseOptions: vi.fn(),
}));

vi.mock('../../../services', async (importOriginal) => {
  const actual = await importOriginal<typeof Services>();
  return { ...actual, api: { ...actual.api, ...backend } };
});

const { useMuseStore } = await import('./museStore');

const ok = <T>(data: T) => ({ success: true, data, code: ResponseCode.OK });
const job = { id: 'j1', prompt: 'x' } as MuseJob;

describe('museStore', () => {
  beforeEach(() => {
    useMuseStore.setState({
      jobs: [],
      options: null,
      loaded: false,
      loadError: null,
    });
    backend.listMuseJobs.mockReset();
    backend.getMuseOptions.mockReset();
    backend.listMuseJobs.mockResolvedValue(ok([job]));
    backend.getMuseOptions.mockResolvedValue(ok({}));
  });

  // Review 4.1: bulk changes emit an event without a job; it must not crash or
  // put null into the list.
  it('ignores job events without a job', () => {
    useMuseStore.getState().setJob(job);
    useMuseStore.getState().setJob(null);
    expect(useMuseStore.getState().jobs).toEqual([job]);
  });

  it('reports a backend failure instead of showing an empty queue', async () => {
    backend.listMuseJobs.mockResolvedValue({
      success: false,
      error: 'journal unreadable',
      code: ResponseCode.Internal,
    });
    await useMuseStore.getState().load();
    const state = useMuseStore.getState();
    expect(state.loaded).toBe(true);
    expect(state.loadError).toBe('journal unreadable');
  });

  it('loads jobs and options', async () => {
    await useMuseStore.getState().load();
    const state = useMuseStore.getState();
    expect(state.loadError).toBeNull();
    expect(state.jobs).toEqual([job]);
  });

  // Review #40: an event that lands while a list call is in flight is newer
  // than that call's snapshot and must survive it.
  it('keeps events that arrive while the list loads', async () => {
    let answer: (value: unknown) => void = () => {};
    backend.listMuseJobs.mockReturnValue(new Promise((resolve) => (answer = resolve)));
    const loading = useMuseStore.getState().loadJobs();
    const done = { ...job, status: 'succeeded' } as MuseJob;
    const added = { id: 'j2', prompt: 'y' } as MuseJob;
    useMuseStore.getState().setJob(done);
    useMuseStore.getState().setJob(added);
    useMuseStore.getState().dropJob('j3');
    answer(ok([job, { id: 'j3', prompt: 'z' } as MuseJob]));
    await loading;
    const ids = useMuseStore.getState().jobs.map((j) => j.id);
    expect(ids.sort()).toEqual(['j1', 'j2']);
    expect(useMuseStore.getState().jobs.find((j) => j.id === 'j1')?.status).toBe('succeeded');
  });

  it('shares one reload between calls made while it runs', async () => {
    await Promise.all([useMuseStore.getState().loadJobs(), useMuseStore.getState().loadJobs()]);
    expect(backend.listMuseJobs).toHaveBeenCalledTimes(1);
    expect(backend.getMuseOptions).not.toHaveBeenCalled();
  });

  // An account check reads Muse's limits; the screens pick them up without
  // reloading the jobs, and a failed reload keeps what was known.
  it('reloads the options alone and keeps them on failure', async () => {
    const known = { known: true, maxFiles: 10 };
    backend.getMuseOptions.mockResolvedValue(ok(known));
    await Promise.all([
      useMuseStore.getState().loadOptions(),
      useMuseStore.getState().loadOptions(),
    ]);
    expect(backend.getMuseOptions).toHaveBeenCalledTimes(1);
    expect(backend.listMuseJobs).not.toHaveBeenCalled();
    expect(useMuseStore.getState().options).toEqual(known);

    backend.getMuseOptions.mockRejectedValue(new Error('down'));
    await useMuseStore.getState().loadOptions();
    expect(useMuseStore.getState().options).toEqual(known);
  });

  it('treats a null job list as empty', async () => {
    backend.listMuseJobs.mockResolvedValue(ok(null));
    await useMuseStore.getState().loadJobs();
    expect(useMuseStore.getState().jobs).toEqual([]);
    expect(useMuseStore.getState().loadError).toBeNull();
  });
});
