/**
 * Backend contract re-exported from the Wails-generated bindings.
 *
 * Every DTO and enum here is generated from Go (frontend/wailsjs/go/models.ts).
 * Never redeclare these shapes by hand: change the Go type, then run `wails dev`,
 * `wails build` or `wails generate module` to regenerate.
 */
import { chrome, config, engine, events, flow, models, session } from '../../wailsjs/go/models';
import type { buildinfo, media, services } from '../../wailsjs/go/models';

export type AppInfo = buildinfo.Info;
export type AppConfig = config.AppConfig;
export const AppConfig = config.AppConfig;
export type SystemInfo = services.SystemInfo;
export type LoginProcessInfo = session.LoginProcessInfo;
export type LoginStatusInfo = session.LoginStatusInfo;
export type AccountProfile = session.AccountProfile;
export type FlowModel = flow.FlowModel;
export const FlowModel = flow.FlowModel;
export type FlowImageProject = flow.FlowImageProject;
export const FlowImageProjectMode = flow.ImageProjectMode;
export type FlowImageProjectMode = flow.ImageProjectMode;
export type FlowProjectImage = flow.FlowProjectImage;
export type FlowProjectImageReference = flow.FlowProjectImageReference;
export const FlowImageQuality = flow.ImageQuality;
export type FlowImageQuality = flow.ImageQuality;
export type GenerateImageRequest = flow.GenerateImageRequest;
export const GenerateImageRequest = flow.GenerateImageRequest;
export type GeneratedImageItem = flow.GeneratedImageItem;
export const GeneratedImageItem = flow.GeneratedImageItem;
export type FlowVideoModel = flow.FlowVideoModel;
export type FlowVideoCost = flow.FlowVideoCost;
export type GenerateVideoRequest = flow.GenerateVideoRequest;
export const GenerateVideoRequest = flow.GenerateVideoRequest;
export type GeneratedVideoItem = flow.GeneratedVideoItem;
export type SaveMediaResult = media.SaveResult;
export type MediaDirectoryResult = media.DirectoryResult;
export type ImportVideoResult = media.ImportResult;
export type FlowEditModel = flow.FlowEditModel;
export type EditVideoRequest = flow.EditVideoRequest;
export const EditVideoRequest = flow.EditVideoRequest;
export type UploadedSourceVideo = flow.UploadedSourceVideo;
export const UploadedSourceVideo = flow.UploadedSourceVideo;
export type StudioConfig = flow.StudioConfig;
export type AccountConfig = config.AccountConfig;
export type NodeSpec = engine.Spec;
export const PortType = engine.PortType;
export type PortType = engine.PortType;
export const ParamKind = engine.ParamKind;
export type ParamKind = engine.ParamKind;
export const AccountStatus = session.AccountStatus;
export type AccountStatus = session.AccountStatus;
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
