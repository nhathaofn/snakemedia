export namespace accounts {
	
	export enum Provider {
	    Flow = "flow",
	    Muse = "muse",
	}
	export enum Source {
	    Browser = "browser",
	    Cookies = "cookies",
	}
	export enum Status {
	    Error = "error",
	    SignedIn = "signed_in",
	    SignedOut = "signed_out",
	    SigningIn = "signing_in",
	    Unverified = "unverified",
	}
	export class UsageItem {
	    label: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new UsageItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.value = source["value"];
	    }
	}
	export class Account {
	    id: string;
	    provider: Provider;
	    name: string;
	    identity?: string;
	    source: Source;
	    status: Status;
	    enabled: boolean;
	    usage: UsageItem[];
	    proxy: string;
	    proxyInUse?: string;
	    proxyRotating?: boolean;
	    proxyStatus?: string;
	    proxyIp?: string;
	    pausedUntil?: string;
	    message?: string;
	    noRoute?: string;
	    lastCheckedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Account(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.provider = source["provider"];
	        this.name = source["name"];
	        this.identity = source["identity"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.enabled = source["enabled"];
	        this.usage = this.convertValues(source["usage"], UsageItem);
	        this.proxy = source["proxy"];
	        this.proxyInUse = source["proxyInUse"];
	        this.proxyRotating = source["proxyRotating"];
	        this.proxyStatus = source["proxyStatus"];
	        this.proxyIp = source["proxyIp"];
	        this.pausedUntil = source["pausedUntil"];
	        this.message = source["message"];
	        this.noRoute = source["noRoute"];
	        this.lastCheckedAt = source["lastCheckedAt"];
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

}

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

}

export namespace config {
	
	export enum LogLevel {
	    Debug = "debug",
	    Error = "error",
	    Info = "info",
	    Warn = "warn",
	}
	export enum MuseMode {
	    Rotate = "rotate",
	    Sequential = "sequential",
	}
	export enum Theme {
	    Dark = "dark",
	    Light = "light",
	    System = "system",
	}
	export class AccountConfig {
	    headless: boolean;
	    pageReadySeconds: number;
	    pollIntervalMs: number;
	    loginTimeoutSeconds: number;
	    requestTimeoutSeconds: number;
	    browserStartupTimeoutSeconds: number;
	    maxResponseBytes: number;
	    maxDownloadBytes: number;
	    idleBrowserMinutes: number;
	
	    static createFrom(source: any = {}) {
	        return new AccountConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.headless = source["headless"];
	        this.pageReadySeconds = source["pageReadySeconds"];
	        this.pollIntervalMs = source["pollIntervalMs"];
	        this.loginTimeoutSeconds = source["loginTimeoutSeconds"];
	        this.requestTimeoutSeconds = source["requestTimeoutSeconds"];
	        this.browserStartupTimeoutSeconds = source["browserStartupTimeoutSeconds"];
	        this.maxResponseBytes = source["maxResponseBytes"];
	        this.maxDownloadBytes = source["maxDownloadBytes"];
	        this.idleBrowserMinutes = source["idleBrowserMinutes"];
	    }
	}
	export class MuseConfig {
	    maxAttempts: number;
	    maxRechecks: number;
	    imageTimeoutMinutes: number;
	    videoTimeoutMinutes: number;
	    pollIntervalMs: number;
	    pollErrorLimit: number;
	    sendGapMinSeconds: number;
	    sendGapMaxSeconds: number;
	    sendTimeoutSeconds: number;
	    maxJobs: number;
	    maxAccounts: number;
	    totalThreads: number;
	    threadsPerAccount: number;
	    mode: MuseMode;
	    useTopup: boolean;
	    usagePauseMinutes: number;
	    autoSave: boolean;
	    outputDir: string;
	
	    static createFrom(source: any = {}) {
	        return new MuseConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxAttempts = source["maxAttempts"];
	        this.maxRechecks = source["maxRechecks"];
	        this.imageTimeoutMinutes = source["imageTimeoutMinutes"];
	        this.videoTimeoutMinutes = source["videoTimeoutMinutes"];
	        this.pollIntervalMs = source["pollIntervalMs"];
	        this.pollErrorLimit = source["pollErrorLimit"];
	        this.sendGapMinSeconds = source["sendGapMinSeconds"];
	        this.sendGapMaxSeconds = source["sendGapMaxSeconds"];
	        this.sendTimeoutSeconds = source["sendTimeoutSeconds"];
	        this.maxJobs = source["maxJobs"];
	        this.maxAccounts = source["maxAccounts"];
	        this.totalThreads = source["totalThreads"];
	        this.threadsPerAccount = source["threadsPerAccount"];
	        this.mode = source["mode"];
	        this.useTopup = source["useTopup"];
	        this.usagePauseMinutes = source["usagePauseMinutes"];
	        this.autoSave = source["autoSave"];
	        this.outputDir = source["outputDir"];
	    }
	}
	export class FlowConfig {
	    probePath: string;
	    uploadAttempts: number;
	    uploadRetryDelayMs: number;
	    maxPollFailures: number;
	    pollMaxIntervalMs: number;
	    maxImageBytes: number;
	    modelCacheSeconds: number;
	    jobRetentionHours: number;
	    generationTimeoutSeconds: number;
	    videoGenerationTimeoutSeconds: number;
	    videoPollIntervalMs: number;
	    maxConcurrentRequestsPerAccount: number;
	    signedOutReads: number;
	    queueThreads: number;
	    queueGapMinSeconds: number;
	    queueGapMaxSeconds: number;
	    queueMaxTasks: number;
	    maxDiscoveryBytes: number;
	    maxDiscoveryScripts: number;
	    rpcCacheSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.probePath = source["probePath"];
	        this.uploadAttempts = source["uploadAttempts"];
	        this.uploadRetryDelayMs = source["uploadRetryDelayMs"];
	        this.maxPollFailures = source["maxPollFailures"];
	        this.pollMaxIntervalMs = source["pollMaxIntervalMs"];
	        this.maxImageBytes = source["maxImageBytes"];
	        this.modelCacheSeconds = source["modelCacheSeconds"];
	        this.jobRetentionHours = source["jobRetentionHours"];
	        this.generationTimeoutSeconds = source["generationTimeoutSeconds"];
	        this.videoGenerationTimeoutSeconds = source["videoGenerationTimeoutSeconds"];
	        this.videoPollIntervalMs = source["videoPollIntervalMs"];
	        this.maxConcurrentRequestsPerAccount = source["maxConcurrentRequestsPerAccount"];
	        this.signedOutReads = source["signedOutReads"];
	        this.queueThreads = source["queueThreads"];
	        this.queueGapMinSeconds = source["queueGapMinSeconds"];
	        this.queueGapMaxSeconds = source["queueGapMaxSeconds"];
	        this.queueMaxTasks = source["queueMaxTasks"];
	        this.maxDiscoveryBytes = source["maxDiscoveryBytes"];
	        this.maxDiscoveryScripts = source["maxDiscoveryScripts"];
	        this.rpcCacheSeconds = source["rpcCacheSeconds"];
	    }
	}
	export class AppConfig {
	    theme: Theme;
	    logLevel: LogLevel;
	    windowScale: number;
	    accounts: AccountConfig;
	    flow: FlowConfig;
	    muse: MuseConfig;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	        this.windowScale = source["windowScale"];
	        this.accounts = this.convertValues(source["accounts"], AccountConfig);
	        this.flow = this.convertValues(source["flow"], FlowConfig);
	        this.muse = this.convertValues(source["muse"], MuseConfig);
	        this.revision = source["revision"];
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
	    Toggle = "toggle",
	}
	export enum PortType {
	    Image = "image",
	    Media = "media",
	    Text = "text",
	    Video = "video",
	}
	export class Endpoint {
	    node: string;
	    port: string;
	
	    static createFrom(source: any = {}) {
	        return new Endpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = source["node"];
	        this.port = source["port"];
	    }
	}
	export class Edge {
	    from: Endpoint;
	    to: Endpoint;
	
	    static createFrom(source: any = {}) {
	        return new Edge(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = this.convertValues(source["from"], Endpoint);
	        this.to = this.convertValues(source["to"], Endpoint);
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
	
	export class GraphNode {
	    id: string;
	    type: string;
	    params?: number[];
	
	    static createFrom(source: any = {}) {
	        return new GraphNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.params = source["params"];
	    }
	}
	export class Graph {
	    requestId?: string;
	    nodes: GraphNode[];
	    edges: Edge[];
	
	    static createFrom(source: any = {}) {
	        return new Graph(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.nodes = this.convertValues(source["nodes"], GraphNode);
	        this.edges = this.convertValues(source["edges"], Edge);
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
	
	export class NodeRun {
	    status: string;
	    output?: number[];
	    error?: string;
	    code?: models.Code;
	    jobIds?: string[];
	
	    static createFrom(source: any = {}) {
	        return new NodeRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.code = source["code"];
	        this.jobIds = source["jobIds"];
	    }
	}
	export class GraphResult {
	    order: string[];
	    nodes: Record<string, NodeRun>;
	
	    static createFrom(source: any = {}) {
	        return new GraphResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.order = source["order"];
	        this.nodes = this.convertValues(source["nodes"], NodeRun, true);
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
	
	export class Option {
	    value: string;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new Option(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.label = source["label"];
	    }
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
	    AccountsUpdated = "accounts:updated",
	    AppReady = "app:ready",
	    ConfigUpdated = "config:updated",
	    FlowImageTaskUpdated = "flow:image-task-updated",
	    FlowImageTasksChanged = "flow:image-tasks-changed",
	    MuseJobUpdated = "muse:job-updated",
	    MuseJobsChanged = "muse:jobs-changed",
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
	export enum ImageTaskStatus {
	    Cancelled = "cancelled",
	    Failed = "failed",
	    Queued = "queued",
	    Running = "running",
	    Succeeded = "succeeded",
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
	    maxCredits: number;
	
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
	        this.maxCredits = source["maxCredits"];
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
	export class FlowVideoCost {
	    duration: number;
	    resolution: string;
	    credits: number;
	
	    static createFrom(source: any = {}) {
	        return new FlowVideoCost(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.duration = source["duration"];
	        this.resolution = source["resolution"];
	        this.credits = source["credits"];
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
	    costs: FlowVideoCost[];
	
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
	        this.costs = this.convertValues(source["costs"], FlowVideoCost);
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
	    requestId?: string;
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
	        this.requestId = source["requestId"];
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
	    requestId?: string;
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
	        this.requestId = source["requestId"];
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
	    jobId?: string;
	    pending?: boolean;
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
	        this.jobId = source["jobId"];
	        this.pending = source["pending"];
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
	export class ImageTaskReference {
	    localUrl: string;
	    fileName: string;
	    mimeType: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new ImageTaskReference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localUrl = source["localUrl"];
	        this.fileName = source["fileName"];
	        this.mimeType = source["mimeType"];
	        this.size = source["size"];
	    }
	}
	export class ImageTask {
	    id: string;
	    batchId: string;
	    prompt: string;
	    modelId: string;
	    aspectRatio: string;
	    quality: ImageQuality;
	    count: number;
	    references: ImageTaskReference[];
	    outputDir: string;
	    held?: boolean;
	    status: ImageTaskStatus;
	    step?: string;
	    requestId?: string;
	    rechecks?: number;
	    nextCheckAt?: string;
	    results: GeneratedImageItem[];
	    savedFiles: string[];
	    error?: string;
	    createdAt: string;
	    startedAt?: string;
	    finishedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageTask(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.batchId = source["batchId"];
	        this.prompt = source["prompt"];
	        this.modelId = source["modelId"];
	        this.aspectRatio = source["aspectRatio"];
	        this.quality = source["quality"];
	        this.count = source["count"];
	        this.references = this.convertValues(source["references"], ImageTaskReference);
	        this.outputDir = source["outputDir"];
	        this.held = source["held"];
	        this.status = source["status"];
	        this.step = source["step"];
	        this.requestId = source["requestId"];
	        this.rechecks = source["rechecks"];
	        this.nextCheckAt = source["nextCheckAt"];
	        this.results = this.convertValues(source["results"], GeneratedImageItem);
	        this.savedFiles = source["savedFiles"];
	        this.error = source["error"];
	        this.createdAt = source["createdAt"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
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
	export class ImageQueueState {
	    tasks: ImageTask[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageQueueState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tasks = this.convertValues(source["tasks"], ImageTask);
	        this.warning = source["warning"];
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
	
	
	export class ImageTaskRequest {
	    prompts: string[];
	    modelId: string;
	    aspectRatio: string;
	    quality: ImageQuality;
	    count: number;
	    references: ImageTaskReference[];
	    outputDir: string;
	    hold: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImageTaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.prompts = source["prompts"];
	        this.modelId = source["modelId"];
	        this.aspectRatio = source["aspectRatio"];
	        this.quality = source["quality"];
	        this.count = source["count"];
	        this.references = this.convertValues(source["references"], ImageTaskReference);
	        this.outputDir = source["outputDir"];
	        this.hold = source["hold"];
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
	
	export class StudioConfig {
	    videoExtensions: string[];
	    imageAspectRatios: string[];
	    defaultImageRatio: string;
	    defaultBatchRatio: string;
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
	        this.defaultBatchRatio = source["defaultBatchRatio"];
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
	    Conflict = 409,
	    Internal = 500,
	    InvalidArgument = 400,
	    NotFound = 404,
	    OK = 200,
	    Unavailable = 503,
	}
	export class Response_SnakeMedia_internal_accounts_Account_ {
	    success: boolean;
	    data: accounts.Account;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_accounts_Account_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], accounts.Account);
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
	export class Response_SnakeMedia_internal_engine_GraphResult_ {
	    success: boolean;
	    data: engine.GraphResult;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_engine_GraphResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], engine.GraphResult);
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
	export class Response_SnakeMedia_internal_providers_flow_ImageQueueState_ {
	    success: boolean;
	    data: flow.ImageQueueState;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_ImageQueueState_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.ImageQueueState);
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
	export class Response_SnakeMedia_internal_providers_flow_ImageTaskReference_ {
	    success: boolean;
	    data: flow.ImageTaskReference;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_ImageTaskReference_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.ImageTaskReference);
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
	export class Response_SnakeMedia_internal_providers_flow_ImageTask_ {
	    success: boolean;
	    data: flow.ImageTask;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_flow_ImageTask_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.ImageTask);
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
	export class Response_SnakeMedia_internal_providers_muse_MuseJob_ {
	    success: boolean;
	    data: muse.MuseJob;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_muse_MuseJob_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], muse.MuseJob);
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
	export class Response_SnakeMedia_internal_providers_muse_MuseOptions_ {
	    success: boolean;
	    data: muse.MuseOptions;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_providers_muse_MuseOptions_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], muse.MuseOptions);
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
	export class Response_SnakeMedia_internal_proxy_CheckResult_ {
	    success: boolean;
	    data: proxy.CheckResult;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_proxy_CheckResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], proxy.CheckResult);
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
	export class Response___SnakeMedia_internal_accounts_Account_ {
	    success: boolean;
	    data: accounts.Account[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_accounts_Account_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], accounts.Account);
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
	export class Response___SnakeMedia_internal_engine_Option_ {
	    success: boolean;
	    data: engine.Option[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_engine_Option_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], engine.Option);
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
	export class Response___SnakeMedia_internal_providers_flow_ImageTaskReference_ {
	    success: boolean;
	    data: flow.ImageTaskReference[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_ImageTaskReference_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.ImageTaskReference);
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
	export class Response___SnakeMedia_internal_providers_flow_ImageTask_ {
	    success: boolean;
	    data: flow.ImageTask[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_flow_ImageTask_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], flow.ImageTask);
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
	export class Response___SnakeMedia_internal_providers_muse_MuseJob_ {
	    success: boolean;
	    data: muse.MuseJob[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_muse_MuseJob_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], muse.MuseJob);
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
	export class Response___SnakeMedia_internal_providers_muse_MuseLibraryItem_ {
	    success: boolean;
	    data: muse.MuseLibraryItem[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_muse_MuseLibraryItem_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], muse.MuseLibraryItem);
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
	export class Response___SnakeMedia_internal_providers_muse_MuseReference_ {
	    success: boolean;
	    data: muse.MuseReference[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_providers_muse_MuseReference_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], muse.MuseReference);
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
	export class Response___SnakeMedia_internal_proxy_CheckResult_ {
	    success: boolean;
	    data: proxy.CheckResult[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_proxy_CheckResult_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], proxy.CheckResult);
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

export namespace muse {
	
	export enum MuseJobKind {
	    Image = "image",
	    Video = "video",
	}
	export enum MuseJobStatus {
	    Cancelled = "cancelled",
	    Failed = "failed",
	    Processing = "processing",
	    Queued = "queued",
	    Succeeded = "succeeded",
	}
	export class MuseJob {
	    id: string;
	    kind: MuseJobKind;
	    prompt: string;
	    label?: string;
	    references: string[];
	    rechecks?: number;
	    held?: boolean;
	    status: MuseJobStatus;
	    step?: string;
	    accountId?: string;
	    sessionId?: string;
	    sending?: boolean;
	    attempts: number;
	    error?: string;
	    files: string[];
	    savedFiles?: string[];
	    createdAt: string;
	    startedAt?: string;
	    sentAt?: string;
	    finishedAt?: string;
	    limitReplies?: number;
	    nextCheckAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new MuseJob(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.prompt = source["prompt"];
	        this.label = source["label"];
	        this.references = source["references"];
	        this.rechecks = source["rechecks"];
	        this.held = source["held"];
	        this.status = source["status"];
	        this.step = source["step"];
	        this.accountId = source["accountId"];
	        this.sessionId = source["sessionId"];
	        this.sending = source["sending"];
	        this.attempts = source["attempts"];
	        this.error = source["error"];
	        this.files = source["files"];
	        this.savedFiles = source["savedFiles"];
	        this.createdAt = source["createdAt"];
	        this.startedAt = source["startedAt"];
	        this.sentAt = source["sentAt"];
	        this.finishedAt = source["finishedAt"];
	        this.limitReplies = source["limitReplies"];
	        this.nextCheckAt = source["nextCheckAt"];
	    }
	}
	export class MuseJobRequest {
	    kind: MuseJobKind;
	    prompts: string[];
	    repeat: number;
	    references: string[];
	    label: string;
	    held: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MuseJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.prompts = source["prompts"];
	        this.repeat = source["repeat"];
	        this.references = source["references"];
	        this.label = source["label"];
	        this.held = source["held"];
	    }
	}
	export class MuseLibraryItem {
	    id: string;
	    jobId: string;
	    kind: MuseJobKind;
	    localUrl: string;
	    savedPath?: string;
	    prompt: string;
	    label?: string;
	    accountId?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new MuseLibraryItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.jobId = source["jobId"];
	        this.kind = source["kind"];
	        this.localUrl = source["localUrl"];
	        this.savedPath = source["savedPath"];
	        this.prompt = source["prompt"];
	        this.label = source["label"];
	        this.accountId = source["accountId"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class MuseModel {
	    id: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new MuseModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	    }
	}
	export class MuseOptions {
	    known: boolean;
	    maxFileBytes: number;
	    maxFiles: number;
	    imageExtensions: string[];
	    models: MuseModel[];
	    checkedAt: string;
	    journalWarning: string;
	
	    static createFrom(source: any = {}) {
	        return new MuseOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.known = source["known"];
	        this.maxFileBytes = source["maxFileBytes"];
	        this.maxFiles = source["maxFiles"];
	        this.imageExtensions = source["imageExtensions"];
	        this.models = this.convertValues(source["models"], MuseModel);
	        this.checkedAt = source["checkedAt"];
	        this.journalWarning = source["journalWarning"];
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
	export class MuseReference {
	    localUrl: string;
	    fileName: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new MuseReference(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.localUrl = source["localUrl"];
	        this.fileName = source["fileName"];
	        this.size = source["size"];
	    }
	}

}

export namespace proxy {
	
	export class CheckResult {
	    raw: string;
	    status: string;
	    ok: boolean;
	    ip?: string;
	    port?: number;
	    latencyMs?: number;
	    message?: string;
	    nextChangeSeconds?: number;
	    checkedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.raw = source["raw"];
	        this.status = source["status"];
	        this.ok = source["ok"];
	        this.ip = source["ip"];
	        this.port = source["port"];
	        this.latencyMs = source["latencyMs"];
	        this.message = source["message"];
	        this.nextChangeSeconds = source["nextChangeSeconds"];
	        this.checkedAt = source["checkedAt"];
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

