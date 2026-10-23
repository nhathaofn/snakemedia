import { afterEach, describe, expect, it, vi } from 'vitest';
import { ResponseCode } from '../types';
import { ApiError, invoke, unwrap } from './api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('invoke', () => {
  it('reports Unavailable outside the Wails runtime without calling the binding', async () => {
    const call = vi.fn();
    const res = await invoke('GetConfig', call);
    expect(call).not.toHaveBeenCalled();
    expect(res.success).toBe(false);
    expect(res.code).toBe(ResponseCode.Unavailable);
  });

  it('converts thrown errors into an Internal response', async () => {
    vi.stubGlobal('window', { go: {}, runtime: {} });
    const res = await invoke('GetConfig', () => Promise.reject(new Error('boom')));
    expect(res).toEqual({ success: false, error: 'GetConfig: boom', code: ResponseCode.Internal });
  });

  it('passes successful responses through', async () => {
    vi.stubGlobal('window', { go: {}, runtime: {} });
    const ok = { success: true, data: 42, code: ResponseCode.OK };
    await expect(invoke('X', () => Promise.resolve(ok))).resolves.toBe(ok);
  });
});

describe('unwrap', () => {
  it('returns data on success', () => {
    expect(unwrap({ success: true, data: 'v', code: ResponseCode.OK })).toBe('v');
  });

  it('throws ApiError carrying the backend code', () => {
    const fn = () => unwrap({ success: false, error: 'bad', code: ResponseCode.InvalidArgument });
    expect(fn).toThrow(ApiError);
    expect(fn).toThrow('bad');
  });
});
