import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ResponseCode } from '../../../types';
import type * as Services from '../../../services';
import type { MuseJob } from '../../../types';

const backend = vi.hoisted(() => ({
  listMuseJobs: vi.fn(),
  getMuseAccounts: vi.fn(),
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
      accounts: [],
      options: null,
      loaded: false,
      loadError: null,
    });
    backend.listMuseJobs.mockResolvedValue(ok([job]));
    backend.getMuseAccounts.mockResolvedValue(ok([]));
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

  it('loads jobs, accounts and options', async () => {
    await useMuseStore.getState().load();
    const state = useMuseStore.getState();
    expect(state.loadError).toBeNull();
    expect(state.jobs).toEqual([job]);
  });
});
