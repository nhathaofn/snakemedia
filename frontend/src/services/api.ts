import * as Backend from '../../wailsjs/go/main/App';
import { ResponseCode } from '../types';
import type {
  AccountProfile,
  ApiResponse,
  AppConfig,
  AppInfo,
  EditVideoRequest,
  FlowEditModel,
  FlowImageProject,
  FlowImageProjectMode,
  FlowModel,
  FlowProjectImage,
  FlowProjectImageReference,
  FlowVideoModel,
  GenerateImageRequest,
  GenerateVideoRequest,
  GeneratedImageItem,
  GeneratedVideoItem,
  ImportVideoResult,
  LoginProcessInfo,
  LoginStatusInfo,
  MediaDirectoryResult,
  NodeSpec,
  SaveMediaResult,
  StudioConfig,
  SystemInfo,
  UploadedSourceVideo,
} from '../types';
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
  launchLoginBrowser: (sessionID: string) =>
    invoke<LoginProcessInfo>('LaunchLoginBrowser', () => Backend.LaunchLoginBrowser(sessionID)),
  checkLoginStatus: (sessionID: string) =>
    invoke<LoginStatusInfo>('CheckLoginStatus', () => Backend.CheckLoginStatus(sessionID)),
  cancelLogin: (sessionID: string) =>
    invoke<boolean>('CancelLogin', () => Backend.CancelLogin(sessionID)),
  getAccounts: () => invoke<AccountProfile[]>('GetAccounts', Backend.GetAccounts),
  removeAccount: (id: string) => invoke<boolean>('RemoveAccount', () => Backend.RemoveAccount(id)),
  refreshAccount: (id: string) =>
    invoke<AccountProfile>('RefreshAccount', () => Backend.RefreshAccount(id)),
  openAccountBrowser: (id: string) =>
    invoke<LoginProcessInfo>('OpenAccountBrowser', () => Backend.OpenAccountBrowser(id)),
  getImageModels: () => invoke<FlowModel[]>('GetImageModels', Backend.GetImageModels),
  getActiveRotationCount: () =>
    invoke<number>('GetActiveRotationCount', Backend.GetActiveRotationCount),
  generateImages: (req: GenerateImageRequest) =>
    invoke<GeneratedImageItem[]>('GenerateImages', () => Backend.GenerateImages(req)),

  getVideoModels: () => invoke<FlowVideoModel[]>('GetVideoModels', Backend.GetVideoModels),
  generateVideos: (req: GenerateVideoRequest) =>
    invoke<GeneratedVideoItem[]>('GenerateVideos', () => Backend.GenerateVideos(req)),
  listVideoEditJobs: () =>
    invoke<GeneratedVideoItem[]>('ListVideoEditJobs', Backend.ListVideoEditJobs),
  resumeVideoEdit: (id: string) =>
    invoke<GeneratedVideoItem[]>('ResumeVideoEdit', () => Backend.ResumeVideoEdit(id)),
  getEditModels: () => invoke<FlowEditModel[]>('GetEditModels', Backend.GetEditModels),
  editVideos: (req: EditVideoRequest) =>
    invoke<GeneratedVideoItem[]>('EditVideos', () => Backend.EditVideos(req)),
  pickVideo: () => invoke<ImportVideoResult>('PickVideo', Backend.PickVideo),
  uploadSourceVideo: (localUrl: string) =>
    invoke<UploadedSourceVideo>('UploadSourceVideo', () => Backend.UploadSourceVideo(localUrl)),
  getStudioConfig: () => invoke<StudioConfig>('GetStudioConfig', Backend.GetStudioConfig),
  listFlowImageProjects: (mode: FlowImageProjectMode) =>
    invoke<FlowImageProject[]>('ListFlowImageProjects', () => Backend.ListFlowImageProjects(mode)),
  listFlowProjectImages: (accountID: string, projectID: string, mode: FlowImageProjectMode) =>
    invoke<FlowProjectImage[]>('ListFlowProjectImages', () =>
      Backend.ListFlowProjectImages(accountID, projectID, mode),
    ),
  getFlowProjectImageReference: (
    accountID: string,
    projectID: string,
    mode: FlowImageProjectMode,
    mediaID: string,
  ) =>
    invoke<FlowProjectImageReference>('GetFlowProjectImageReference', () =>
      Backend.GetFlowProjectImageReference(accountID, projectID, mode, mediaID),
    ),
  getDefaultImageOutputDirectory: () =>
    invoke<string>('GetDefaultImageOutputDirectory', Backend.GetDefaultImageOutputDirectory),
  listNodes: () => invoke<NodeSpec[]>('ListNodes', Backend.ListNodes),
  saveMedia: (localUrl: string) =>
    invoke<SaveMediaResult>('SaveMedia', () => Backend.SaveMedia(localUrl)),
  chooseImageOutputDirectory: (defaultDir: string) =>
    invoke<MediaDirectoryResult>('ChooseImageOutputDirectory', () =>
      Backend.ChooseImageOutputDirectory(defaultDir),
    ),
  saveImageToTaskFolder: (localUrl: string, outputDir: string, taskId: string) =>
    invoke<SaveMediaResult>('SaveImageToTaskFolder', () =>
      Backend.SaveImageToTaskFolder(localUrl, outputDir, taskId),
    ),
};
