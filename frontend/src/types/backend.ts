/**
 * Backend contract re-exported from the Wails-generated bindings.
 *
 * Every DTO and enum here is generated from Go (frontend/wailsjs/go/models.ts).
 * Never redeclare these shapes by hand: change the Go type, then run `wails dev`,
 * `wails build` or `wails generate module` to regenerate.
 */
import { chrome, config, events, models, services } from '../../wailsjs/go/models';
import type { buildinfo } from '../../wailsjs/go/models';

export type AppInfo = buildinfo.Info;
export type AppConfig = config.AppConfig;
export const AppConfig = config.AppConfig;
export type SystemInfo = services.SystemInfo;
export type LoginProcessInfo = services.LoginProcessInfo;
export type LoginStatusInfo = services.LoginStatusInfo;
export type AccountProfile = services.AccountProfile;
export type AccountConfig = config.AccountConfig;
export const AccountStatus = services.AccountStatus;
export type AccountStatus = services.AccountStatus;
export const AccountTier = chrome.Tier;
export type AccountTier = chrome.Tier;

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
