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
	export class GlossaryEntry {
	    enabled: boolean;
	    source: string;
	    target: string;
	    caseSensitive?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GlossaryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.source = source["source"];
	        this.target = source["target"];
	        this.caseSensitive = source["caseSensitive"];
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
	    glossary?: GlossaryEntry[];
	
	    static createFrom(source: any = {}) {
	        return new PipelineSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preRules = this.convertValues(source["preRules"], Rule);
	        this.postRules = this.convertValues(source["postRules"], Rule);
	        this.glossary = this.convertValues(source["glossary"], GlossaryEntry);
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
	export class LlamaCppSettings {
	    binaryPath: string;
	    modelPath: string;
	    port: number;
	    gpuLayers: number;
	    ctxSize: number;
	    autoStart: boolean;
	    extraArgs: string;
	
	    static createFrom(source: any = {}) {
	        return new LlamaCppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.binaryPath = source["binaryPath"];
	        this.modelPath = source["modelPath"];
	        this.port = source["port"];
	        this.gpuLayers = source["gpuLayers"];
	        this.ctxSize = source["ctxSize"];
	        this.autoStart = source["autoStart"];
	        this.extraArgs = source["extraArgs"];
	    }
	}
	export class OfflineLLMSettings {
	    baseUrl: string;
	    model: string;
	    template: string;
	    sourceLang: string;
	    targetLang: string;
	    temperature: number;
	
	    static createFrom(source: any = {}) {
	        return new OfflineLLMSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseUrl = source["baseUrl"];
	        this.model = source["model"];
	        this.template = source["template"];
	        this.sourceLang = source["sourceLang"];
	        this.targetLang = source["targetLang"];
	        this.temperature = source["temperature"];
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
	export class TraditionalSettings {
	    sourceLang: string;
	    targetLang: string;
	
	    static createFrom(source: any = {}) {
	        return new TraditionalSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceLang = source["sourceLang"];
	        this.targetLang = source["targetLang"];
	    }
	}
	export class TranslatorSettings {
	    active: string;
	    bing: BingSettings;
	    traditional: TraditionalSettings;
	    llm: LLMSettings;
	    offlineLlm: OfflineLLMSettings;
	    llamaCpp: LlamaCppSettings;
	
	    static createFrom(source: any = {}) {
	        return new TranslatorSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.bing = this.convertValues(source["bing"], BingSettings);
	        this.traditional = this.convertValues(source["traditional"], TraditionalSettings);
	        this.llm = this.convertValues(source["llm"], LLMSettings);
	        this.offlineLlm = this.convertValues(source["offlineLlm"], OfflineLLMSettings);
	        this.llamaCpp = this.convertValues(source["llamaCpp"], LlamaCppSettings);
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
	    stripTypewriterTags: boolean;
	
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
	        this.stripTypewriterTags = source["stripTypewriterTags"];
	    }
	}
	export class Config {
	    embed: EmbedSettings;
	    translator: TranslatorSettings;
	    pipeline: PipelineSettings;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.embed = this.convertValues(source["embed"], EmbedSettings);
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
	export class EmbedHook {
	    hookCode: string;
	    hookName: string;
	    ctx: number;
	    ctx2: number;
	
	    static createFrom(source: any = {}) {
	        return new EmbedHook(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hookCode = source["hookCode"];
	        this.hookName = source["hookName"];
	        this.ctx = source["ctx"];
	        this.ctx2 = source["ctx2"];
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
	    supported: boolean;
	
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
	        this.supported = source["supported"];
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

export namespace pipeline {
	
	export class CacheEntry {
	    gameUid: string;
	    provider: string;
	    sourceText: string;
	    translated: string;
	    manual: boolean;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new CacheEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gameUid = source["gameUid"];
	        this.provider = source["provider"];
	        this.sourceText = source["sourceText"];
	        this.translated = source["translated"];
	        this.manual = source["manual"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace session {
	
	export class FunctionHealthStats {
	    func: string;
	    subsystem: string;
	    calls: number;
	    errors: number;
	    timeouts: number;
	    lastDurUs: number;
	    maxDurUs: number;
	    avgDurUs: number;
	    lastError?: string;
	    lastCalledAt: string;
	
	    static createFrom(source: any = {}) {
	        return new FunctionHealthStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.func = source["func"];
	        this.subsystem = source["subsystem"];
	        this.calls = source["calls"];
	        this.errors = source["errors"];
	        this.timeouts = source["timeouts"];
	        this.lastDurUs = source["lastDurUs"];
	        this.maxDurUs = source["maxDurUs"];
	        this.avgDurUs = source["avgDurUs"];
	        this.lastError = source["lastError"];
	        this.lastCalledAt = source["lastCalledAt"];
	    }
	}
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
	export class SavedGame {
	    exePath: string;
	    title: string;
	    embedHooks: config.EmbedHook[];
	    pipeline: config.PipelineSettings;
	    runningPid: number;
	    attached: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SavedGame(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exePath = source["exePath"];
	        this.title = source["title"];
	        this.embedHooks = this.convertValues(source["embedHooks"], config.EmbedHook);
	        this.pipeline = this.convertValues(source["pipeline"], config.PipelineSettings);
	        this.runningPid = source["runningPid"];
	        this.attached = source["attached"];
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
	export class TelemetryEvent {
	    seq: number;
	    timestamp: string;
	    unixMs: number;
	    level: string;
	    subsystem: string;
	    func: string;
	    msg: string;
	    durationUs?: number;
	    fields?: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new TelemetryEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.timestamp = source["timestamp"];
	        this.unixMs = source["unixMs"];
	        this.level = source["level"];
	        this.subsystem = source["subsystem"];
	        this.func = source["func"];
	        this.msg = source["msg"];
	        this.durationUs = source["durationUs"];
	        this.fields = source["fields"];
	    }
	}
	export class TelemetrySnapshot {
	    events: TelemetryEvent[];
	    stats: FunctionHealthStats[];
	    droppedEvents: number;
	
	    static createFrom(source: any = {}) {
	        return new TelemetrySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.events = this.convertValues(source["events"], TelemetryEvent);
	        this.stats = this.convertValues(source["stats"], FunctionHealthStats);
	        this.droppedEvents = source["droppedEvents"];
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
	export class UnityFontStatus {
	    installed: boolean;
	    count: number;
	    dir: string;
	    downloading: boolean;
	    progress: number;
	    message: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new UnityFontStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.count = source["count"];
	        this.dir = source["dir"];
	        this.downloading = source["downloading"];
	        this.progress = source["progress"];
	        this.message = source["message"];
	        this.error = source["error"];
	    }
	}

}

export namespace translator {
	
	export class LlamaCppStatus {
	    running: boolean;
	    ready: boolean;
	    pid: number;
	    port: number;
	    baseUrl: string;
	    modelPath: string;
	    modelName: string;
	    lastError: string;
	    recentLogs: string;
	
	    static createFrom(source: any = {}) {
	        return new LlamaCppStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.ready = source["ready"];
	        this.pid = source["pid"];
	        this.port = source["port"];
	        this.baseUrl = source["baseUrl"];
	        this.modelPath = source["modelPath"];
	        this.modelName = source["modelName"];
	        this.lastError = source["lastError"];
	        this.recentLogs = source["recentLogs"];
	    }
	}

}

