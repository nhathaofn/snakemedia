/**
 * Backend contract re-exported from the Wails-generated bindings.
 *
 * Every DTO and enum here is generated from Go (frontend/wailsjs/go/models.ts).
 * Never redeclare these shapes by hand: change the Go type, then run `wails dev`,
 * `wails build` or `wails generate module` to regenerate.
 */
import { config, events, models } from '../../wailsjs/go/models';
import type { buildinfo, services } from '../../wailsjs/go/models';

export type AppInfo = buildinfo.Info;
export type AppConfig = config.AppConfig;
export type SystemInfo = services.SystemInfo;

export const Theme = config.Theme;
export type Theme = config.Theme;

export const LogLevel = config.LogLevel;
export type LogLevel = config.LogLevel;

export const AppEvent = events.Event;
export type AppEvent = events.Event;

export const ResponseCode = models.Code;
export type ResponseCode = models.Code;

/** Mirrors Go `models.Response[T]`. */
export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: string;
  code: ResponseCode;
}
