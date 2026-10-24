export namespace buildinfo {
	
	export class Info {
	    name: string;
	    productName: string;
	    companyName: string;
	    version: string;
	    commit: string;
	    buildDate: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.productName = source["productName"];
	        this.companyName = source["companyName"];
	        this.version = source["version"];
	        this.commit = source["commit"];
	        this.buildDate = source["buildDate"];
	    }
	}

}

export namespace chrome {
	
	export enum Tier {
	    Exempt = "EXEMPT",
	    Free = "FREE",
	    Gemnova = "GEMNOVA",
	    Pro = "PRO",
	    Ultra = "ULTRA",
	    Ultra1P5 = "ULTRA1P5",
	    Unknown = "UNSPECIFIED",
	    Unsubscribed = "UNSUBSCRIBED_WITH_CREDITS",
	    Zero = "ZERO",
	}
	export class FlowCredits {
	    availabilityCode?: number;
	    credits: number;
	    tierCode: number;
	    tier: Tier;
	    resetStatus: number;
	    quotaFlag: number;
	    dailyLimit: number;
	    resetAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowCredits(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.availabilityCode = source["availabilityCode"];
	        this.credits = source["credits"];
	        this.tierCode = source["tierCode"];
	        this.tier = source["tier"];
	        this.resetStatus = source["resetStatus"];
	        this.quotaFlag = source["quotaFlag"];
	        this.dailyLimit = source["dailyLimit"];
	        this.resetAt = source["resetAt"];
	    }
	}

}

export namespace config {
	
	export enum LogLevel {
	    Debug = "debug",
	    Error = "error",
	    Info = "info",
	    Warn = "warn",
	}
	export enum Theme {
	    Dark = "dark",
	    Light = "light",
	    System = "system",
	}
	export class AccountConfig {
	    pollIntervalMs: number;
	    pollMaxIntervalMs: number;
	    loginTimeoutSeconds: number;
	    requestTimeoutSeconds: number;
	    browserStartupTimeoutSeconds: number;
	    refreshCooldownSeconds: number;
	    maxResponseBytes: number;
	    maxDiscoveryBytes: number;
	    maxDiscoveryScripts: number;
	    generationTimeoutSeconds: number;
	    videoGenerationTimeoutSeconds: number;
	    videoPollIntervalMs: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pollIntervalMs = source["pollIntervalMs"];
	        this.pollMaxIntervalMs = source["pollMaxIntervalMs"];
	        this.loginTimeoutSeconds = source["loginTimeoutSeconds"];
	        this.requestTimeoutSeconds = source["requestTimeoutSeconds"];
	        this.browserStartupTimeoutSeconds = source["browserStartupTimeoutSeconds"];
	        this.refreshCooldownSeconds = source["refreshCooldownSeconds"];
	        this.maxResponseBytes = source["maxResponseBytes"];
	        this.maxDiscoveryBytes = source["maxDiscoveryBytes"];
	        this.maxDiscoveryScripts = source["maxDiscoveryScripts"];
	        this.generationTimeoutSeconds = source["generationTimeoutSeconds"];
	        this.videoGenerationTimeoutSeconds = source["videoGenerationTimeoutSeconds"];
	        this.videoPollIntervalMs = source["videoPollIntervalMs"];
	    }
	}
	export class FlowConfig {
	    probePath: string;
	    maxSourceBytes: number;
	    maxDownloadBytes: number;
	    uploadAttempts: number;
	    uploadRetryDelayMs: number;
	    maxPollFailures: number;
	    pollMaxIntervalMs: number;
	    minTrimSeconds: number;
	    trimStepSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.probePath = source["probePath"];
	        this.maxSourceBytes = source["maxSourceBytes"];
	        this.maxDownloadBytes = source["maxDownloadBytes"];
	        this.uploadAttempts = source["uploadAttempts"];
	        this.uploadRetryDelayMs = source["uploadRetryDelayMs"];
	        this.maxPollFailures = source["maxPollFailures"];
	        this.pollMaxIntervalMs = source["pollMaxIntervalMs"];
	        this.minTrimSeconds = source["minTrimSeconds"];
	        this.trimStepSeconds = source["trimStepSeconds"];
	    }
	}
	export class AppConfig {
	    theme: Theme;
	    logLevel: LogLevel;
	    windowScale: number;
	    cdpPort: number;
	    accounts: AccountConfig;
	    flow: FlowConfig;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	        this.windowScale = source["windowScale"];
	        this.cdpPort = source["cdpPort"];
	        this.accounts = this.convertValues(source["accounts"], AccountConfig);
	        this.flow = this.convertValues(source["flow"], FlowConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace engine {
	
	export enum ParamKind {
	    Integer = "integer",
	    Select = "select",
	    Text = "text",
	}
	export enum PortType {
	    Image = "image",
	    Media = "media",
	    Text = "text",
	    Video = "video",
	}
	export class Param {
	    name: string;
	    label: string;
	    kind: ParamKind;
	    optionsFrom?: string;
	    min?: number;
	    max?: number;
	
	    static createFrom(source: any = {}) {
	        return new Param(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.kind = source["kind"];
	        this.optionsFrom = source["optionsFrom"];
	        this.min = source["min"];
	        this.max = source["max"];
	    }
	}
	export class Port {
	    name: string;
	    label: string;
	    type: PortType;
	    multiple: boolean;
	    optional: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Port(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.multiple = source["multiple"];
	        this.optional = source["optional"];
	    }
	}
	export class Spec {
	    type: string;
	    provider: string;
	    label: string;
	    description: string;
	    inputs: Port[];
	    outputs: Port[];
	    params: Param[];
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.provider = source["provider"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.inputs = this.convertValues(source["inputs"], Port);
	        this.outputs = this.convertValues(source["outputs"], Port);
	        this.params = this.convertValues(source["params"], Param);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace events {
	
	export enum Event {
	    AppReady = "app:ready",
	    ConfigUpdated = "config:updated",
	    ErrorOccurred = "error:occurred",
	}

}

export namespace flow {
	
	export enum ImageProjectMode {
	    All = "all",
	    Single = "single",
	}
	export enum ImageQuality {
	    FourK = "4K",
	    OneK = "1K",
	    TwoK = "2K",
	}
	export class EditVideoRequest {
	    sourceId?: string;
	    requestId?: string;
	    prompt: string;
	    modelId: string;
	    projectId?: string;
	    sourceUrl: string;
	    trimStartMs: number;
	    trimEndMs: number;
	
	    static createFrom(source: any = {}) {
	        return new EditVideoRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceId = source["sourceId"];
	        this.requestId = source["requestId"];
	        this.prompt = source["prompt"];
	        this.modelId = source["modelId"];
	        this.projectId = source["projectId"];
	        this.sourceUrl = source["sourceUrl"];
	        this.trimStartMs = source["trimStartMs"];
	        this.trimEndMs = source["trimEndMs"];
	    }
	}
	export class FlowEditModel {
	    id: string;
	    name: string;
	    default: boolean;
	    duration: number;
	    maxInputSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowEditModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.default = source["default"];
	        this.duration = source["duration"];
	        this.maxInputSeconds = source["maxInputSeconds"];
	    }
	}
	export class FlowImageProject {
	    accountId: string;
	    accountEmail: string;
	    projectId: string;
	    mode: ImageProjectMode;
	    title: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowImageProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.accountId = source["accountId"];
	        this.accountEmail = source["accountEmail"];
	        this.projectId = source["projectId"];
	        this.mode = source["mode"];
	        this.title = source["title"];
	    }
	}
	export class FlowModel {
	    id: string;
	    name: string;
	    description: string;
	    default: boolean;
	    availableQualities: string[];
	    maxReferenceImages: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.default = source["default"];
	        this.availableQualities = source["availableQualities"];
	        this.maxReferenceImages = source["maxReferenceImages"];
	    }
	}
	export class FlowProjectImage {
	    id: string;
	    name: string;
	    previewUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowProjectImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.previewUrl = source["previewUrl"];
	    }
	}
	export class FlowProjectImageReference {
	    base64: string;
	    mimeType: string;
	    fileName: string;
	
	    static createFrom(source: any = {}) {
	        return new FlowProjectImageReference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base64 = source["base64"];
	        this.mimeType = source["mimeType"];
	        this.fileName = source["fileName"];
	    }
	}
	export class FlowVideoModel {
	    id: string;
	    name: string;
	    default: boolean;
	    durations: number[];
	    aspectRatios: string[];
	    resolutions: string[];
	    maxReferenceImages: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowVideoModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.default = source["default"];
	        this.durations = source["durations"];
	        this.aspectRatios = source["aspectRatios"];
	        this.resolutions = source["resolutions"];
	        this.maxReferenceImages = source["maxReferenceImages"];
	    }
	}
	export class ReferenceImage {
	    base64: string;
	    mimeType: string;
	    fileName: string;
	
	    static createFrom(source: any = {}) {
	        return new ReferenceImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.base64 = source["base64"];
	        this.mimeType = source["mimeType"];
	        this.fileName = source["fileName"];
	    }
	}
	export class GenerateImageRequest {
	    prompt: string;
	    modelId: string;
	    aspectRatio: string;
	    quality?: ImageQuality;
	    projectMode?: ImageProjectMode;
	    count: number;
	    references?: ReferenceImage[];
	    projectId?: string;
	
	    static createFrom(source: any = {}) {
	        return new GenerateImageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt = source["prompt"];
	        this.modelId = source["modelId"];
	        this.aspectRatio = source["aspectRatio"];
	        this.quality = source["quality"];
	        this.projectMode = source["projectMode"];
	        this.count = source["count"];
	        this.references = this.convertValues(source["references"], ReferenceImage);
	        this.projectId = source["projectId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GenerateVideoRequest {
	    prompt: string;
	    modelId: string;
	    aspectRatio: string;
	    duration: number;
	    resolution: string;
	    count: number;
	    projectId?: string;
	    references?: ReferenceImage[];
	
	    static createFrom(source: any = {}) {
	        return new GenerateVideoRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompt = source["prompt"];
	        this.modelId = source["modelId"];
	        this.aspectRatio = source["aspectRatio"];
	        this.duration = source["duration"];
	        this.resolution = source["resolution"];
	        this.count = source["count"];
	        this.projectId = source["projectId"];
	        this.references = this.convertValues(source["references"], ReferenceImage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GeneratedImageItem {
	    id: string;
	    url: string;
	    prompt: string;
	    model: string;
	    aspectRatio: string;
	    createdAt: string;
	    modelName: string;
	    localUrl?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GeneratedImageItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.prompt = source["prompt"];
	        this.model = source["model"];
	        this.aspectRatio = source["aspectRatio"];
	        this.createdAt = source["createdAt"];
	        this.modelName = source["modelName"];
	        this.localUrl = source["localUrl"];
	        this.error = source["error"];
	    }
	}
	export class GeneratedVideoItem {
	    jobId?: string;
	    pending?: boolean;
	    id: string;
	    prompt: string;
	    model: string;
	    aspectRatio: string;
	    duration: number;
	    resolution: string;
	    createdAt: string;
	    modelName: string;
	    localUrl?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new GeneratedVideoItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobId = source["jobId"];
	        this.pending = source["pending"];
	        this.id = source["id"];
	        this.prompt = source["prompt"];
	        this.model = source["model"];
	        this.aspectRatio = source["aspectRatio"];
	        this.duration = source["duration"];
	        this.resolution = source["resolution"];
	        this.createdAt = source["createdAt"];
	        this.modelName = source["modelName"];
	        this.localUrl = source["localUrl"];
	        this.error = source["error"];
	    }
	}
	
	export class StudioConfig {
	    videoExtensions: string[];
	    imageAspectRatios: string[];
	    defaultImageRatio: string;
	    maxImages: number;
	    maxVideos: number;
	    maxReferenceBytes: number;
	    referenceMimeTypes: string[];
	
	    static createFrom(source: any = {}) {
	        return new StudioConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.videoExtensions = source["videoExtensions"];
	        this.imageAspectRatios = source["imageAspectRatios"];
	        this.defaultImageRatio = source["defaultImageRatio"];
	        this.maxImages = source["maxImages"];
	        this.maxVideos = source["maxVideos"];
	        this.maxReferenceBytes = source["maxReferenceBytes"];
	        this.referenceMimeTypes = source["referenceMimeTypes"];
	    }
	}
	export class UploadedSourceVideo {
	    minTrimSeconds: number;
	    trimStepSeconds: number;
	    sourceId: string;
	    durationSeconds: number;
	    models: FlowEditModel[];
	    displayName: string;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new UploadedSourceVideo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minTrimSeconds = source["minTrimSeconds"];
	        this.trimStepSeconds = source["trimStepSeconds"];
	        this.sourceId = source["sourceId"];
	        this.durationSeconds = source["durationSeconds"];
	        this.models = this.convertValues(source["models"], FlowEditModel);
	        this.displayName = source["displayName"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace media {
	
	export class DirectoryResult {
	    path: string;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DirectoryResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.canceled = source["canceled"];
	    }
	}
	export class ImportResult {
	    localUrl: string;
	    fileName: string;
	    size: number;
	    contentType: string;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localUrl = source["localUrl"];
	        this.fileName = source["fileName"];
	        this.size = source["size"];
	        this.contentType = source["contentType"];
	        this.canceled = source["canceled"];
	    }
	}
	export class SaveResult {
	    path: string;
	    canceled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.canceled = source["canceled"];
	    }
	}

}

export namespace models {
	
	export enum Code {
	    Internal = 500,
	    InvalidArgument = 400,
	    NotFound = 404,
	    OK = 200,
	    Unavailable = 503,
	}
	export class Response_SnakeMedia_internal_buildinfo_Info_ {
	    success: boolean;
	    data: buildinfo.Info;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_buildinfo_Info_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], buildinfo.Info);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_config_AppConfig_ {
	    success: boolean;
	    data: config.AppConfig;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_config_AppConfig_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], config.AppConfig);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_media_DirectoryResult_ {
	    success: boolean;
	    data: media.DirectoryResult;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_media_DirectoryResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], media.DirectoryResult);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_media_ImportResult_ {
	    success: boolean;
	    data: media.ImportResult;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_media_ImportResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], media.ImportResult);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_media_SaveResult_ {
	    success: boolean;
	    data: media.SaveResult;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_media_SaveResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], media.SaveResult);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_FlowProjectImageReference_ {
	    success: boolean;
	    data: flow.FlowProjectImageReference;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_FlowProjectImageReference_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowProjectImageReference);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_StudioConfig_ {
	    success: boolean;
	    data: flow.StudioConfig;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_StudioConfig_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.StudioConfig);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_UploadedSourceVideo_ {
	    success: boolean;
	    data: flow.UploadedSourceVideo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_UploadedSourceVideo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.UploadedSourceVideo);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_session_AccountProfile_ {
	    success: boolean;
	    data: session.AccountProfile;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_session_AccountProfile_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], session.AccountProfile);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_session_LoginProcessInfo_ {
	    success: boolean;
	    data: session.LoginProcessInfo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_session_LoginProcessInfo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], session.LoginProcessInfo);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_providers_flow_session_LoginStatusInfo_ {
	    success: boolean;
	    data: session.LoginStatusInfo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_session_LoginStatusInfo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], session.LoginStatusInfo);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_SnakeMedia_internal_services_SystemInfo_ {
	    success: boolean;
	    data: services.SystemInfo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_services_SystemInfo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], services.SystemInfo);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_engine_Spec_ {
	    success: boolean;
	    data: engine.Spec[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_engine_Spec_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], engine.Spec);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_FlowEditModel_ {
	    success: boolean;
	    data: flow.FlowEditModel[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_FlowEditModel_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowEditModel);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_FlowImageProject_ {
	    success: boolean;
	    data: flow.FlowImageProject[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_FlowImageProject_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowImageProject);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_FlowModel_ {
	    success: boolean;
	    data: flow.FlowModel[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_FlowModel_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowModel);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_FlowProjectImage_ {
	    success: boolean;
	    data: flow.FlowProjectImage[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_FlowProjectImage_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowProjectImage);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_FlowVideoModel_ {
	    success: boolean;
	    data: flow.FlowVideoModel[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_FlowVideoModel_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.FlowVideoModel);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_GeneratedImageItem_ {
	    success: boolean;
	    data: flow.GeneratedImageItem[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_GeneratedImageItem_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.GeneratedImageItem);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_GeneratedVideoItem_ {
	    success: boolean;
	    data: flow.GeneratedVideoItem[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_GeneratedVideoItem_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.GeneratedVideoItem);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response___SnakeMedia_internal_providers_flow_session_AccountProfile_ {
	    success: boolean;
	    data: session.AccountProfile[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_session_AccountProfile_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], session.AccountProfile);
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Response_bool_ {
	    success: boolean;
	    data: boolean;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_bool_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = source["data"];
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	}
	export class Response_int_ {
	    success: boolean;
	    data: number;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_int_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = source["data"];
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	}
	export class Response_string_ {
	    success: boolean;
	    data: string;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_string_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = source["data"];
	        this.error = source["error"];
	        this.code = source["code"];
	    }
	}

}

export namespace services {
	
	export class SystemInfo {
	    os: string;
	    arch: string;
	    goVersion: string;
	    uptime: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.os = source["os"];
	        this.arch = source["arch"];
	        this.goVersion = source["goVersion"];
	        this.uptime = source["uptime"];
	    }
	}

}

export namespace session {
	
	export enum AccountStatus {
	    Connected = "connected",
	    Error = "error",
	    Expired = "expired",
	    SignedIn = "signed_in",
	    Unverified = "unverified",
	}
	export class AccountProfile {
	    id: string;
	    email: string;
	    gaiaId: string;
	    name: string;
	    profileDir: string;
	    status: AccountStatus;
	    balance?: chrome.FlowCredits;
	    balanceError?: string;
	    cookieCount: number;
	    lastVerifiedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.email = source["email"];
	        this.gaiaId = source["gaiaId"];
	        this.name = source["name"];
	        this.profileDir = source["profileDir"];
	        this.status = source["status"];
	        this.balance = this.convertValues(source["balance"], chrome.FlowCredits);
	        this.balanceError = source["balanceError"];
	        this.cookieCount = source["cookieCount"];
	        this.lastVerifiedAt = source["lastVerifiedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LoginProcessInfo {
	    sessionId: string;
	    port: number;
	    profileDir: string;
	    expiresAt: string;
	    policy: config.AccountConfig;
	
	    static createFrom(source: any = {}) {
	        return new LoginProcessInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.port = source["port"];
	        this.profileDir = source["profileDir"];
	        this.expiresAt = source["expiresAt"];
	        this.policy = this.convertValues(source["policy"], config.AccountConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LoginStatusInfo {
	    isConnected: boolean;
	    isLoggedIn: boolean;
	    profile?: AccountProfile;
	    browserVersion?: string;
	    activePage?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isConnected = source["isConnected"];
	        this.isLoggedIn = source["isLoggedIn"];
	        this.profile = this.convertValues(source["profile"], AccountProfile);
	        this.browserVersion = source["browserVersion"];
	        this.activePage = source["activePage"];
	        this.message = source["message"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

