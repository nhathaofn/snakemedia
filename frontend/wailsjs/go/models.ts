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
	    maxDiscoveryScripts: number;
	
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
	        this.maxDiscoveryScripts = source["maxDiscoveryScripts"];
	    }
	}
	export class AppConfig {
	    theme: Theme;
	    logLevel: LogLevel;
	    windowScale: number;
	    cdpPort: number;
	    accounts: AccountConfig;
	
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
	export class Response_SnakeMedia_internal_services_AccountProfile_ {
	    success: boolean;
	    data: services.AccountProfile;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_services_AccountProfile_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], services.AccountProfile);
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
	export class Response_SnakeMedia_internal_services_LoginProcessInfo_ {
	    success: boolean;
	    data: services.LoginProcessInfo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_services_LoginProcessInfo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], services.LoginProcessInfo);
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
	export class Response_SnakeMedia_internal_services_LoginStatusInfo_ {
	    success: boolean;
	    data: services.LoginStatusInfo;
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response_SnakeMedia_internal_services_LoginStatusInfo_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], services.LoginStatusInfo);
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
	export class Response___SnakeMedia_internal_services_AccountProfile_ {
	    success: boolean;
	    data: services.AccountProfile[];
	    error?: string;
	    code: Code;
	
	    static createFrom(source: any = {}) {
	        return new Response___SnakeMedia_internal_services_AccountProfile_(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.data = this.convertValues(source["data"], services.AccountProfile);
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

}

export namespace services {
	
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

