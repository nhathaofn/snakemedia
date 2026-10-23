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
	export class AppConfig {
	    theme: Theme;
	    logLevel: LogLevel;
	    windowScale: number;
	
	    static createFrom(source: any = {}) {
	        return new AppConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.theme = source["theme"];
	        this.logLevel = source["logLevel"];
	        this.windowScale = source["windowScale"];
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
	    data?: buildinfo.Info;
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
	    data?: config.AppConfig;
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
	export class Response_SnakeMedia_internal_services_SystemInfo_ {
	    success: boolean;
	    data?: services.SystemInfo;
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

