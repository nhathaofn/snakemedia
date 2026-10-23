import * as Backend from '../../wailsjs/go/main/App';
import { ResponseCode } from '../types';
import type { ApiResponse, AppConfig, AppInfo, SystemInfo } from '../types';
import { errorMessage } from '../utils';
import { isWailsRuntime } from './runtime';

/** Error thrown by `unwrap` when a backend call did not succeed. */
export class ApiError extends Error {
  readonly code: ResponseCode;

  constructor(message: string, code: ResponseCode) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
  }
}

/**
 * Calls a generated Go binding and normalises every failure mode
 * (no Wails runtime, thrown error) into an ApiResponse envelope.
 */
export async function invoke<T>(
  method: string,
  call: () => Promise<ApiResponse<T>>,
): Promise<ApiResponse<T>> {
  if (!isWailsRuntime()) {
    return {
      success: false,
      error: `${method}: Wails runtime is not available (start the app with \`wails dev\`)`,
      code: ResponseCode.Unavailable,
    };
  }
  try {
    return await call();
  } catch (err) {
    return {
      success: false,
      error: `${method}: ${errorMessage(err)}`,
      code: ResponseCode.Internal,
    };
  }
}

/** Returns `data` of a successful response, otherwise throws ApiError. */
export function unwrap<T>(res: ApiResponse<T>): T {
  if (!res.success || res.data === undefined) {
    throw new ApiError(res.error ?? 'Unknown backend error', res.code);
  }
  return res.data;
}

/** Typed bridge to the Go backend. Add one entry per exported App method. */
export const api = {
  getAppInfo: () => invoke<AppInfo>('GetAppInfo', Backend.GetAppInfo),
  getSystemInfo: () => invoke<SystemInfo>('GetSystemInfo', Backend.GetSystemInfo),
  getConfig: () => invoke<AppConfig>('GetConfig', Backend.GetConfig),
  updateConfig: (cfg: AppConfig) =>
    invoke<AppConfig>('UpdateConfig', () => Backend.UpdateConfig(cfg)),
};
