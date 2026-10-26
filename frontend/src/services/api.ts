import * as Backend from '../../wailsjs/go/main/App';
import type { muse } from '../../wailsjs/go/models';
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
  MuseAccount,
  MuseJob,
  MuseJobRequest,
  MuseOptions,
  MuseReference,
  NodeSpec,
  Production,
  ProxyCheckResult,
  SaveMediaResult,
  StudioAsset,
  StudioConfig,
  StudioData,
  StudioProject,
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

/** Envelopes `invoke` built from a rejected binding call rather than a Go reply. */
const transportFailures = new WeakSet<object>();

/**
 * True when the call failed in the IPC bridge (or Go panicked) instead of
 * returning a response: the backend may or may not have acted on it.
 */
export function isTransportFailure(res: ApiResponse<unknown>): boolean {
  return transportFailures.has(res);
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
    const failure: ApiResponse<T> = {
      success: false,
      error: `${method}: ${errorMessage(err)}`,
      code: ResponseCode.Internal,
    };
    transportFailures.add(failure);
    return failure;
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
  resumeImageGeneration: (id: string) =>
    invoke<GeneratedImageItem[]>('ResumeImageGeneration', () => Backend.ResumeImageGeneration(id)),
  listPendingImageGenerations: (mode: FlowImageProjectMode) =>
    invoke<GeneratedImageItem[]>('ListPendingImageGenerations', () =>
      Backend.ListPendingImageGenerations(mode),
    ),

  getVideoModels: () => invoke<FlowVideoModel[]>('GetVideoModels', Backend.GetVideoModels),
  generateVideos: (req: GenerateVideoRequest) =>
    invoke<GeneratedVideoItem[]>('GenerateVideos', () => Backend.GenerateVideos(req)),
  resumeVideoGeneration: (id: string) =>
    invoke<GeneratedVideoItem[]>('ResumeVideoGeneration', () => Backend.ResumeVideoGeneration(id)),
  listPendingVideoGenerations: () =>
    invoke<GeneratedVideoItem[]>(
      'ListPendingVideoGenerations',
      Backend.ListPendingVideoGenerations,
    ),
  listVideoEditJobs: () =>
    invoke<GeneratedVideoItem[]>('ListVideoEditJobs', Backend.ListVideoEditJobs),
  cancelGeneration: (requestId: string) =>
    invoke<boolean>('CancelGeneration', () => Backend.CancelGeneration(requestId)),
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

  getMuseOptions: () => invoke<MuseOptions>('GetMuseOptions', Backend.GetMuseOptions),
  getMuseAccounts: () => invoke<MuseAccount[]>('GetMuseAccounts', Backend.GetMuseAccounts),
  addMuseAccount: (name: string) =>
    invoke<MuseAccount>('AddMuseAccount', () => Backend.AddMuseAccount(name)),
  importMuseCookies: (name: string, cookies: string) =>
    invoke<MuseAccount>('ImportMuseCookies', () => Backend.ImportMuseCookies(name, cookies)),
  replaceMuseCookies: (id: string, cookies: string) =>
    invoke<MuseAccount>('ReplaceMuseCookies', () => Backend.ReplaceMuseCookies(id, cookies)),
  checkMuseAccount: (id: string) =>
    invoke<MuseAccount>('CheckMuseAccount', () => Backend.CheckMuseAccount(id)),
  openMuseAccountBrowser: (id: string) =>
    invoke<MuseAccount>('OpenMuseAccountBrowser', () => Backend.OpenMuseAccountBrowser(id)),
  setMuseAccountEnabled: (id: string, enabled: boolean) =>
    invoke<MuseAccount>('SetMuseAccountEnabled', () => Backend.SetMuseAccountEnabled(id, enabled)),
  removeMuseAccount: (id: string) =>
    invoke<boolean>('RemoveMuseAccount', () => Backend.RemoveMuseAccount(id)),
  pickMuseReferences: () =>
    invoke<MuseReference[]>('PickMuseReferences', Backend.PickMuseReferences),
  submitMuseJobs: (req: MuseJobRequest) =>
    invoke<MuseJob[]>('SubmitMuseJobs', () => Backend.SubmitMuseJobs(req)),
  listMuseJobs: () => invoke<MuseJob[]>('ListMuseJobs', Backend.ListMuseJobs),
  cancelMuseJob: (id: string) => invoke<MuseJob>('CancelMuseJob', () => Backend.CancelMuseJob(id)),
  retryMuseJob: (id: string) => invoke<MuseJob>('RetryMuseJob', () => Backend.RetryMuseJob(id)),
  removeMuseJob: (id: string) => invoke<boolean>('RemoveMuseJob', () => Backend.RemoveMuseJob(id)),
  runMuseJobs: (ids: string[]) => invoke<number>('RunMuseJobs', () => Backend.RunMuseJobs(ids)),
  holdMuseQueued: () => invoke<number>('HoldMuseQueued', Backend.HoldMuseQueued),
  retryMuseFailed: () => invoke<number>('RetryMuseFailed', Backend.RetryMuseFailed),
  clearMuseFinished: () => invoke<number>('ClearMuseFinished', Backend.ClearMuseFinished),
  removeMuseJobs: (ids: string[]) =>
    invoke<number>('RemoveMuseJobs', () => Backend.RemoveMuseJobs(ids)),
  editAndRetryMuseJob: (id: string, prompt: string) =>
    invoke<MuseJob>('EditAndRetryMuseJob', () => Backend.EditAndRetryMuseJob(id, prompt)),
  openMuseOutputFolder: () => invoke<boolean>('OpenMuseOutputFolder', Backend.OpenMuseOutputFolder),
  revealMuseFile: (path: string) =>
    invoke<boolean>('RevealMuseFile', () => Backend.RevealMuseFile(path)),
  getMuseProxies: () => invoke<string>('GetMuseProxies', Backend.GetMuseProxies),
  saveMuseProxies: (text: string) =>
    invoke<boolean>('SaveMuseProxies', () => Backend.SaveMuseProxies(text)),
  checkMuseProxies: () => invoke<ProxyCheckResult[]>('CheckMuseProxies', Backend.CheckMuseProxies),
  setMuseAccountProxy: (id: string, proxyLine: string) =>
    invoke<MuseAccount>('SetMuseAccountProxy', () => Backend.SetMuseAccountProxy(id, proxyLine)),
  rotateMuseAccountIP: (id: string) =>
    invoke<ProxyCheckResult>('RotateMuseAccountIP', () => Backend.RotateMuseAccountIP(id)),
  getMuseStudio: () => invoke<StudioData>('GetMuseStudio', Backend.GetMuseStudio),
  saveMuseStudio: (data: StudioData) =>
    invoke<boolean>('SaveMuseStudio', () =>
      Backend.SaveMuseStudio(data as unknown as muse.StudioData),
    ),
  createMuseStudioProject: (name: string) =>
    invoke<StudioProject>('CreateMuseStudioProject', () => Backend.CreateMuseStudioProject(name)),
  setMuseProjectPaused: (id: string, paused: boolean) =>
    invoke<boolean>('SetMuseProjectPaused', () => Backend.SetMuseProjectPaused(id, paused)),
  setMuseActiveProject: (id: string) =>
    invoke<boolean>('SetMuseActiveProject', () => Backend.SetMuseActiveProject(id)),
  addMuseStudioAsset: (asset: StudioAsset) =>
    invoke<StudioAsset>('AddMuseStudioAsset', () =>
      Backend.AddMuseStudioAsset(asset as unknown as muse.StudioAsset),
    ),
  removeMuseStudioAsset: (id: string) =>
    invoke<boolean>('RemoveMuseStudioAsset', () => Backend.RemoveMuseStudioAsset(id)),
  updateMuseStudioAsset: (asset: StudioAsset) =>
    invoke<StudioAsset>('UpdateMuseStudioAsset', () =>
      Backend.UpdateMuseStudioAsset(asset as unknown as muse.StudioAsset),
    ),
  mergeMuseVideos: (videoPaths: string[], customName: string) =>
    invoke<string>('MergeMuseVideos', () => Backend.MergeMuseVideos(videoPaths, customName)),
  submitMuseProduction: (prod: Production) =>
    invoke<Production>('SubmitMuseProduction', () =>
      Backend.SubmitMuseProduction(prod as unknown as muse.Production),
    ),
  listMuseProductions: () =>
    invoke<Production[]>('ListMuseProductions', Backend.ListMuseProductions),
  approveMuseProductionBoard: (id: string) =>
    invoke<boolean>('ApproveMuseProductionBoard', () => Backend.ApproveMuseProductionBoard(id)),
  retryMuseProduction: (id: string) =>
    invoke<boolean>('RetryMuseProduction', () => Backend.RetryMuseProduction(id)),
  redrawMuseProductionBoard: (id: string) =>
    invoke<boolean>('RedrawMuseProductionBoard', () => Backend.RedrawMuseProductionBoard(id)),
  removeMuseProduction: (id: string) =>
    invoke<boolean>('RemoveMuseProduction', () => Backend.RemoveMuseProduction(id)),
};
