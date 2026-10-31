import * as Backend from '../../wailsjs/go/main/App';
import { ResponseCode } from '../types';
import type {
  Account,
  AccountProvider,
  ApiResponse,
  AppConfig,
  AppInfo,
  EditVideoRequest,
  FlowEditModel,
  FlowImageProject,
  FlowImageProjectMode,
  FlowImageQueueState,
  FlowImageTask,
  FlowImageTaskReference,
  FlowImageTaskRequest,
  FlowModel,
  FlowProjectImage,
  FlowVideoModel,
  GenerateImageRequest,
  GenerateVideoRequest,
  GeneratedImageItem,
  GeneratedVideoItem,
  ImportVideoResult,
  MediaDirectoryResult,
  MuseJob,
  MuseJobKind,
  MuseJobMode,
  MuseJobRequest,
  MuseLibraryItem,
  MuseCleanResult,
  MuseAsset,
  MusePromptCheck,
  MusePromptTemplate,
  MuseOptions,
  MuseReference,
  ProxyCheckResult,
  SaveMediaResult,
  StudioConfig,
  UpdateProgress,
  Tool,
  ToolActivity,
  UpdateRelease,
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

/**
 * `unwrap` for list calls: Go encodes an empty (nil) slice as null, which
 * would otherwise reach callers typed as an array.
 */
export function unwrapList<T>(res: ApiResponse<T[] | null>): T[] {
  return unwrap(res) ?? [];
}

/** Typed bridge to the Go backend. Add one entry per exported App method. */
export const api = {
  getAppInfo: () => invoke<AppInfo>('GetAppInfo', Backend.GetAppInfo),
  checkForUpdate: () => invoke<UpdateRelease>('CheckForUpdate', Backend.CheckForUpdate),
  installUpdate: () => invoke<boolean>('InstallUpdate', Backend.InstallUpdate),
  getUpdateProgress: () => invoke<UpdateProgress>('GetUpdateProgress', Backend.GetUpdateProgress),
  openLogFolder: () => invoke<boolean>('OpenLogFolder', Backend.OpenLogFolder),
  listTools: () => invoke<Tool[]>('ListTools', Backend.ListTools),
  getToolActivity: () => invoke<ToolActivity>('GetToolActivity', Backend.GetToolActivity),
  installTool: (id: string) => invoke<Tool>('InstallTool', () => Backend.InstallTool(id)),
  postponeTool: (id: string) => invoke<boolean>('PostponeTool', () => Backend.PostponeTool(id)),
  removeTool: (id: string) => invoke<boolean>('RemoveTool', () => Backend.RemoveTool(id)),
  getConfig: () => invoke<AppConfig>('GetConfig', Backend.GetConfig),
  updateConfig: (cfg: AppConfig) =>
    invoke<AppConfig>('UpdateConfig', () => Backend.UpdateConfig(cfg)),
  getAccounts: () => invoke<Account[]>('GetAccounts', Backend.GetAccounts),
  addAccount: (provider: AccountProvider, name: string) =>
    invoke<Account>('AddAccount', () => Backend.AddAccount(provider, name)),
  importAccountCookies: (provider: AccountProvider, name: string, cookies: string) =>
    invoke<Account>('ImportAccountCookies', () =>
      Backend.ImportAccountCookies(provider, name, cookies),
    ),
  replaceAccountCookies: (id: string, cookies: string) =>
    invoke<Account>('ReplaceAccountCookies', () => Backend.ReplaceAccountCookies(id, cookies)),
  checkAccount: (id: string) => invoke<Account>('CheckAccount', () => Backend.CheckAccount(id)),
  openAccountBrowser: (id: string) =>
    invoke<Account>('OpenAccountBrowser', () => Backend.OpenAccountBrowser(id)),
  renameAccount: (id: string, name: string) =>
    invoke<Account>('RenameAccount', () => Backend.RenameAccount(id, name)),
  setAccountEnabled: (id: string, enabled: boolean) =>
    invoke<Account>('SetAccountEnabled', () => Backend.SetAccountEnabled(id, enabled)),
  setAccountProxy: (id: string, proxyLine: string) =>
    invoke<Account>('SetAccountProxy', () => Backend.SetAccountProxy(id, proxyLine)),
  rotateAccountIP: (id: string) =>
    invoke<ProxyCheckResult>('RotateAccountIP', () => Backend.RotateAccountIP(id)),
  removeAccount: (id: string) => invoke<boolean>('RemoveAccount', () => Backend.RemoveAccount(id)),
  getProxies: () => invoke<string>('GetProxies', Backend.GetProxies),
  saveProxies: (text: string) => invoke<boolean>('SaveProxies', () => Backend.SaveProxies(text)),
  checkProxies: () => invoke<ProxyCheckResult[]>('CheckProxies', Backend.CheckProxies),
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
  getBatchOutputDirectory: (provider: AccountProvider) =>
    invoke<string>('GetBatchOutputDirectory', () => Backend.GetBatchOutputDirectory(provider)),
  saveMedia: (localUrl: string) =>
    invoke<SaveMediaResult>('SaveMedia', () => Backend.SaveMedia(localUrl)),
  chooseBatchOutputDirectory: (provider: AccountProvider) =>
    invoke<MediaDirectoryResult>('ChooseBatchOutputDirectory', () =>
      Backend.ChooseBatchOutputDirectory(provider),
    ),

  getFlowImageTasks: () =>
    invoke<FlowImageQueueState>('GetFlowImageTasks', Backend.GetFlowImageTasks),
  submitFlowImageTasks: (req: FlowImageTaskRequest) =>
    invoke<FlowImageTask[]>('SubmitFlowImageTasks', () => Backend.SubmitFlowImageTasks(req)),
  runFlowImageTasks: (ids: string[]) =>
    invoke<number>('RunFlowImageTasks', () => Backend.RunFlowImageTasks(ids)),
  holdFlowImageTasks: (ids: string[]) =>
    invoke<number>('HoldFlowImageTasks', () => Backend.HoldFlowImageTasks(ids)),
  cancelFlowImageTask: (id: string) =>
    invoke<FlowImageTask>('CancelFlowImageTask', () => Backend.CancelFlowImageTask(id)),
  retryFlowImageTasks: (ids: string[]) =>
    invoke<number>('RetryFlowImageTasks', () => Backend.RetryFlowImageTasks(ids)),
  editFlowImageTask: (id: string, prompt: string) =>
    invoke<FlowImageTask>('EditFlowImageTask', () => Backend.EditFlowImageTask(id, prompt)),
  removeFlowImageTasks: (ids: string[]) =>
    invoke<number>('RemoveFlowImageTasks', () => Backend.RemoveFlowImageTasks(ids)),
  addFlowImageTaskReferences: (ids: string[], refs: FlowImageTaskReference[]) =>
    invoke<number>('AddFlowImageTaskReferences', () =>
      Backend.AddFlowImageTaskReferences(ids, refs),
    ),
  pickFlowImageReferences: () =>
    invoke<FlowImageTaskReference[]>('PickFlowImageReferences', Backend.PickFlowImageReferences),
  importFlowProjectImageReference: (
    accountID: string,
    projectID: string,
    mode: FlowImageProjectMode,
    mediaID: string,
  ) =>
    invoke<FlowImageTaskReference>('ImportFlowProjectImageReference', () =>
      Backend.ImportFlowProjectImageReference(accountID, projectID, mode, mediaID),
    ),
  getMuseOptions: () => invoke<MuseOptions>('GetMuseOptions', Backend.GetMuseOptions),
  pickMuseReferences: () =>
    invoke<MuseReference[]>('PickMuseReferences', Backend.PickMuseReferences),
  submitMuseJobs: (req: MuseJobRequest) =>
    invoke<MuseJob[]>('SubmitMuseJobs', () => Backend.SubmitMuseJobs(req)),
  listMuseJobs: () => invoke<MuseJob[]>('ListMuseJobs', Backend.ListMuseJobs),
  cancelMuseJob: (id: string) => invoke<MuseJob>('CancelMuseJob', () => Backend.CancelMuseJob(id)),
  retryMuseJob: (id: string) => invoke<MuseJob>('RetryMuseJob', () => Backend.RetryMuseJob(id)),
  removeMuseJob: (id: string) => invoke<boolean>('RemoveMuseJob', () => Backend.RemoveMuseJob(id)),
  runMuseJobs: (ids: string[]) => invoke<number>('RunMuseJobs', () => Backend.RunMuseJobs(ids)),
  holdMuseQueued: (kind: MuseJobKind, mode: MuseJobMode) =>
    invoke<number>('HoldMuseQueued', () => Backend.HoldMuseQueued(kind, mode)),
  retryMuseFailed: (kind: MuseJobKind, mode: MuseJobMode) =>
    invoke<number>('RetryMuseFailed', () => Backend.RetryMuseFailed(kind, mode)),
  clearMuseFinished: (kind: MuseJobKind, mode: MuseJobMode) =>
    invoke<number>('ClearMuseFinished', () => Backend.ClearMuseFinished(kind, mode)),
  removeMuseJobs: (ids: string[]) =>
    invoke<number>('RemoveMuseJobs', () => Backend.RemoveMuseJobs(ids)),
  editAndRetryMuseJob: (id: string, prompt: string) =>
    invoke<MuseJob>('EditAndRetryMuseJob', () => Backend.EditAndRetryMuseJob(id, prompt)),
  openMuseOutputFolder: () => invoke<boolean>('OpenMuseOutputFolder', Backend.OpenMuseOutputFolder),
  revealFile: (path: string) => invoke<boolean>('RevealFile', () => Backend.RevealFile(path)),
  listMuseLibrary: () => invoke<MuseLibraryItem[]>('ListMuseLibrary', Backend.ListMuseLibrary),
  cleanMuseChats: () => invoke<MuseCleanResult[]>('CleanMuseChats', Backend.CleanMuseChats),
  listMuseAssets: () => invoke<MuseAsset[]>('ListMuseAssets', Backend.ListMuseAssets),
  saveMuseAsset: (asset: MuseAsset) =>
    invoke<MuseAsset>('SaveMuseAsset', () => Backend.SaveMuseAsset(asset)),
  removeMuseAssets: (ids: string[]) =>
    invoke<number>('RemoveMuseAssets', () => Backend.RemoveMuseAssets(ids)),
  pickMuseAssetImages: () =>
    invoke<MuseReference[]>('PickMuseAssetImages', Backend.PickMuseAssetImages),
  pickMuseSourceVideo: () =>
    invoke<MuseReference>('PickMuseSourceVideo', Backend.PickMuseSourceVideo),
  checkMusePrompt: (prompt: string, assetIds: string[]) =>
    invoke<MusePromptCheck>('CheckMusePrompt', () => Backend.CheckMusePrompt(prompt, assetIds)),
  listMuseTemplates: () =>
    invoke<MusePromptTemplate[]>('ListMuseTemplates', Backend.ListMuseTemplates),
  saveMuseTemplate: (template: MusePromptTemplate) =>
    invoke<MusePromptTemplate>('SaveMuseTemplate', () => Backend.SaveMuseTemplate(template)),
  removeMuseTemplate: (id: string) =>
    invoke<boolean>('RemoveMuseTemplate', () => Backend.RemoveMuseTemplate(id)),
  removeMuseLibraryItems: (ids: string[]) =>
    invoke<number>('RemoveMuseLibraryItems', () => Backend.RemoveMuseLibraryItems(ids)),
};
