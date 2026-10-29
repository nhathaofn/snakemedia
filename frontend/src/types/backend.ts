/**
 * Backend contract re-exported from the Wails-generated bindings.
 *
 * Every DTO and enum here is generated from Go (frontend/wailsjs/go/models.ts).
 * Never redeclare these shapes by hand: change the Go type, then run `wails dev`,
 * `wails build` or `wails generate module` to regenerate.
 */
import { accounts, config, events, flow, models, muse } from '../../wailsjs/go/models';
import type { buildinfo, media, proxy, updater } from '../../wailsjs/go/models';

export type AppInfo = buildinfo.Info;
export type AppConfig = config.AppConfig;
export const AppConfig = config.AppConfig;
export type Account = accounts.Account;
export const AccountProvider = accounts.Provider;
export type AccountProvider = accounts.Provider;
export const AccountStatus = accounts.Status;
export type AccountStatus = accounts.Status;
export const AccountSource = accounts.Source;
export type AccountSource = accounts.Source;
export type ProxyCheckResult = proxy.CheckResult;
export type FlowModel = flow.FlowModel;
export const FlowModel = flow.FlowModel;
export type FlowImageProject = flow.FlowImageProject;
export const FlowImageProjectMode = flow.ImageProjectMode;
export type FlowImageProjectMode = flow.ImageProjectMode;
export type FlowProjectImage = flow.FlowProjectImage;
export const FlowImageQuality = flow.ImageQuality;
export type FlowImageQuality = flow.ImageQuality;
export type GenerateImageRequest = flow.GenerateImageRequest;
export const GenerateImageRequest = flow.GenerateImageRequest;
export type GeneratedImageItem = flow.GeneratedImageItem;
export const GeneratedImageItem = flow.GeneratedImageItem;
export type FlowImageTask = flow.ImageTask;
export type FlowImageTaskReference = flow.ImageTaskReference;
export type FlowImageTaskRequest = flow.ImageTaskRequest;
export const FlowImageTaskRequest = flow.ImageTaskRequest;
export type FlowImageQueueState = flow.ImageQueueState;
export const FlowImageTaskStatus = flow.ImageTaskStatus;
export type FlowImageTaskStatus = flow.ImageTaskStatus;
export type FlowVideoModel = flow.FlowVideoModel;
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

export type MuseJob = muse.MuseJob;
export type MuseLibraryItem = muse.MuseLibraryItem;
export type MuseCleanResult = muse.MuseCleanResult;
export type MuseOptions = muse.MuseOptions;
export type MuseReference = muse.MuseReference;
export type MuseRatio = muse.MuseRatio;
export type MuseJobRequest = muse.MuseJobRequest;
export const MuseJobRequest = muse.MuseJobRequest;
export const MuseJobKind = muse.MuseJobKind;
export type MuseJobKind = muse.MuseJobKind;
export const MuseJobMode = muse.MuseJobMode;
export type MuseJobMode = muse.MuseJobMode;
export const MuseJobStatus = muse.MuseJobStatus;
export type MuseJobStatus = muse.MuseJobStatus;
export type MuseAsset = muse.MuseAsset;
export const MuseAsset = muse.MuseAsset;
export type MusePromptTemplate = muse.MusePromptTemplate;
export const MusePromptTemplate = muse.MusePromptTemplate;
export type MusePromptCheck = muse.MusePromptCheck;
export const MuseStoryboard = muse.MuseStoryboard;
export const MuseRefRole = muse.MuseRefRole;
export type MuseRefRole = muse.MuseRefRole;
export const MuseRefMode = muse.MuseRefMode;
export type MuseRefMode = muse.MuseRefMode;

export type UpdateRelease = updater.Release;
export type UpdateProgress = updater.Progress;
export type UpdateConfig = config.UpdateConfig;

export type MuseConfig = config.MuseConfig;
export const MuseConfig = config.MuseConfig;

export const MuseMode = config.MuseMode;
export type MuseMode = config.MuseMode;
export const ImageFormat = config.ImageFormat;
export type ImageFormat = config.ImageFormat;

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
