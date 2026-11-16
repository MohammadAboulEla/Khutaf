export namespace config {
	
	export class BingSettings {
	    sourceLang: string;
	    targetLang: string;
	
	    static createFrom(source: any = {}) {
	        return new BingSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceLang = source["sourceLang"];
	        this.targetLang = source["targetLang"];
	    }
	}
	export class Rule {
	    enabled: boolean;
	    regex: boolean;
	    pattern: string;
	    replace: string;
	
	    static createFrom(source: any = {}) {
	        return new Rule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.regex = source["regex"];
	        this.pattern = source["pattern"];
	        this.replace = source["replace"];
	    }
	}
	export class PipelineSettings {
	    preRules: Rule[];
	    postRules: Rule[];
	
	    static createFrom(source: any = {}) {
	        return new PipelineSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preRules = this.convertValues(source["preRules"], Rule);
	        this.postRules = this.convertValues(source["postRules"], Rule);
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
	export class LLMSettings {
	    baseUrl: string;
	    apiKey: string;
	    model: string;
	    systemPrompt: string;
	    temperature: number;
	    sourceLang: string;
	    targetLang: string;
	
	    static createFrom(source: any = {}) {
	        return new LLMSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.apiKey = source["apiKey"];
	        this.model = source["model"];
	        this.systemPrompt = source["systemPrompt"];
	        this.temperature = source["temperature"];
	        this.sourceLang = source["sourceLang"];
	        this.targetLang = source["targetLang"];
	    }
	}
	export class TranslatorSettings {
	    active: string;
	    bing: BingSettings;
	    llm: LLMSettings;
	
	    static createFrom(source: any = {}) {
	        return new TranslatorSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.bing = this.convertValues(source["bing"], BingSettings);
	        this.llm = this.convertValues(source["llm"], LLMSettings);
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
	export class HookSettings {
	    flushDelayMs: number;
	    codepage: number;
	    maxBufferSize: number;
	    maxHistorySize: number;
	
	    static createFrom(source: any = {}) {
	        return new HookSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.flushDelayMs = source["flushDelayMs"];
	        this.codepage = source["codepage"];
	        this.maxBufferSize = source["maxBufferSize"];
	        this.maxHistorySize = source["maxHistorySize"];
	    }
	}
	export class EmbedSettings {
	    timeoutMs: number;
	    displayMode: number;
	    clearText: boolean;
	    changeFont: boolean;
	    fontFamily: string;
	    useUnityBundle: boolean;
	    changeFontSize: boolean;
	    fontSizeRelative: number;
	    limitLineLength: boolean;
	    maxLineLength: number;
	
	    static createFrom(source: any = {}) {
	        return new EmbedSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeoutMs = source["timeoutMs"];
	        this.displayMode = source["displayMode"];
	        this.clearText = source["clearText"];
	        this.changeFont = source["changeFont"];
	        this.fontFamily = source["fontFamily"];
	        this.useUnityBundle = source["useUnityBundle"];
	        this.changeFontSize = source["changeFontSize"];
	        this.fontSizeRelative = source["fontSizeRelative"];
	        this.limitLineLength = source["limitLineLength"];
	        this.maxLineLength = source["maxLineLength"];
	    }
	}
	export class Config {
	    embed: EmbedSettings;
	    hook: HookSettings;
	    translator: TranslatorSettings;
	    pipeline: PipelineSettings;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.embed = this.convertValues(source["embed"], EmbedSettings);
	        this.hook = this.convertValues(source["hook"], HookSettings);
	        this.translator = this.convertValues(source["translator"], TranslatorSettings);
	        this.pipeline = this.convertValues(source["pipeline"], PipelineSettings);
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

export namespace hook {
	
	export class ProcessInfo {
	    pid: number;
	    name: string;
	    path: string;
	    title: string;
	    is64: boolean;
	    engine: string;
	
	    static createFrom(source: any = {}) {
	        return new ProcessInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pid = source["pid"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.title = source["title"];
	        this.is64 = source["is64"];
	        this.engine = source["engine"];
	    }
	}

}

export namespace main {
	
	export class ShapePreview {
	    logical: string;
	    shaped: string;
	    codes: string;
	
	    static createFrom(source: any = {}) {
	        return new ShapePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.logical = source["logical"];
	        this.shaped = source["shaped"];
	        this.codes = source["codes"];
	    }
	}
	export class TranslateResult {
	    translated: string;
	    shaped: string;
	    cached: boolean;
	    elapsedMs: number;
	
	    static createFrom(source: any = {}) {
	        return new TranslateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translated = source["translated"];
	        this.shaped = source["shaped"];
	        this.cached = source["cached"];
	        this.elapsedMs = source["elapsedMs"];
	    }
	}

}

export namespace session {
	
	export class HookView {
	    key: string;
	    hookCode: string;
	    hookName: string;
	    pid: number;
	    addr: number;
	    ctx: number;
	    ctx2: number;
	    embeddable: boolean;
	    embedding: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HookView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.hookCode = source["hookCode"];
	        this.hookName = source["hookName"];
	        this.pid = source["pid"];
	        this.addr = source["addr"];
	        this.ctx = source["ctx"];
	        this.ctx2 = source["ctx2"];
	        this.embeddable = source["embeddable"];
	        this.embedding = source["embedding"];
	    }
	}
	export class Status {
	    hostReady: boolean;
	    hostError: string;
	    attached: boolean;
	    gamePath: string;
	    gameName: string;
	    engine: string;
	    is64: boolean;
	    pids: number[];
	    provider: string;
	    debugMode: boolean;
	    debugRealTrans: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hostReady = source["hostReady"];
	        this.hostError = source["hostError"];
	        this.attached = source["attached"];
	        this.gamePath = source["gamePath"];
	        this.gameName = source["gameName"];
	        this.engine = source["engine"];
	        this.is64 = source["is64"];
	        this.pids = source["pids"];
	        this.provider = source["provider"];
	        this.debugMode = source["debugMode"];
	        this.debugRealTrans = source["debugRealTrans"];
	    }
	}

}

