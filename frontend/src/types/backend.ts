/**
 * Backend contract re-exported from the Wails-generated bindings.
 *
 * Every DTO and enum here is generated from Go (frontend/wailsjs/go/models.ts).
 * Never redeclare these shapes by hand: change the Go type, then run `wails dev`,
 * `wails build` or `wails generate module` to regenerate.
 */
import {
  chrome,
  config,
  engine,
  events,
  flow,
  models,
  muse,
  session,
} from '../../wailsjs/go/models';
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

export type MuseAccount = muse.MuseAccount;
export type MuseQuota = muse.MuseQuota;
export type MuseJob = muse.MuseJob;
export type MuseOptions = muse.MuseOptions;
export type MuseReference = muse.MuseReference;
export type MuseRatioOption = muse.RatioOption;
export type MuseJobRequest = muse.MuseJobRequest;
export const MuseJobRequest = muse.MuseJobRequest;
export const MuseJobKind = muse.MuseJobKind;
export type MuseJobKind = muse.MuseJobKind;
export const MuseJobStatus = muse.MuseJobStatus;
export type MuseJobStatus = muse.MuseJobStatus;
export const MuseAccountStatus = muse.MuseAccountStatus;
export type MuseAccountStatus = muse.MuseAccountStatus;
export const MuseAccountSource = muse.MuseAccountSource;
export type MuseAccountSource = muse.MuseAccountSource;
export type ProxyCheckResult = muse.ProxyCheckResult;

export interface StudioProject {
  id: string;
  name: string;
  paused: boolean;
  createdAt: string;
}

export interface StudioAsset {
  id: string;
  tag: string;
  name: string;
  role: string;
  lockNotes?: string;
  images: string[];
  projectId?: string;
  createdAt: string;
}

export interface PromptTemplate {
  id: string;
  name: string;
  text: string;
}

export interface StudioModelConfig {
  on: boolean;
  images: string[];
}

export interface StudioRotateConfig {
  on: boolean;
  images: string[];
  index: number;
}

export interface StudioData {
  activeProject: string;
  projects: StudioProject[];
  assets: StudioAsset[];
  promptTemplates: PromptTemplate[];
  modelConfig: StudioModelConfig;
  rotateConfig: StudioRotateConfig;
}

export interface SceneItem {
  text: string;
  voiceover?: string;
  img?: string;
  product?: string;
}

export interface ScriptConfig {
  prefix: string;
  ratio: string;
  duration: string;
  voice: string;
  host: string;
  style: string;
  audio: string;
  merge: boolean;
  music: boolean;
  board: string;
  characterTag?: string;
}

export interface VoiceOption {
  value: string;
  label: string;
  gender: string;
}

export interface Production {
  id: string;
  title: string;
  config: ScriptConfig;
  scenes: SceneItem[];
  refs: string[];
  status: string;
  step?: string;
  error?: string;
  boardFiles?: string[];
  threadSessionId?: string;
  threadAccountId?: string;
  currentSceneIdx: number;
  sceneVideoFiles?: string[];
  finalVideoFile?: string;
  boardJobId?: string;
  currentJobId?: string;
  createdAt: string;
  updatedAt: string;
}

export type MuseConfig = config.MuseConfig;
export const MuseConfig = config.MuseConfig;

export const MuseMode = config.MuseMode;
export type MuseMode = config.MuseMode;

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
