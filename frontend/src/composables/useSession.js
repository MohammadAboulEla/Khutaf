import { ref, shallowRef, readonly } from "vue";

const call = (name, ...args) => {
  const fn = window.go?.main?.App?.[name];
  if (!fn) return Promise.resolve(undefined);
  return fn(...args);
};

const on = (event, cb) => {
  const rt = window.runtime;
  if (rt?.EventsOn) rt.EventsOn(event, cb);
};

const status = ref({
  hostReady: false,
  hostError: "",
  attached: false,
  gamePath: "",
  gameName: "",
  engine: "",
  is64: true,
  pids: [],
  provider: "",
  debugMode: false,
  debugRealTrans: true,
});

const processes = ref([]);
const hooks = ref([]);
const hookOutputs = ref({});
const embedFeed = ref([]);
const logs = ref([]);
const debugTraces = ref([]);
const telemetryEvents = ref([]);
const functionStats = ref([]);
const droppedEvents = ref(0);
const savedGames = ref([]);
const cacheCount = ref(0);
const appVersion = ref("");
const pipelineTarget = ref("global");
const config = ref(null);
const unityFontStatus = ref({
  installed: false,
  count: 0,
  dir: "",
  downloading: false,
  progress: 0,
  message: "",
  error: "",
});
const errorBanner = shallowRef("");
const saveState = shallowRef("");

let initialized = false;

function init() {
  if (initialized) return;
  initialized = true;

  on("session:status", (st) => {
    if (st) status.value = st;
  });

  on("unityfonts:progress", (st) => {
    if (st) unityFontStatus.value = st;
  });

  on("hook:new", (h) => {
    const idx = hooks.value.findIndex((x) => x.key === h.key);
    if (idx >= 0) hooks.value[idx] = h;
    else hooks.value.push(h);
  });

  on("hook:remove", (key) => {
    hooks.value = hooks.value.filter((x) => x.key !== key);
  });

  let pendingOutputs = {};
  let pendingFeed = [];
  let pendingLogs = [];
  let pendingTraces = [];
  let flushTimer = null;

  function scheduleFlush() {
    if (flushTimer !== null) return;
    flushTimer = requestAnimationFrame(() => {
      flushTimer = null;
      if (Object.keys(pendingOutputs).length) {
        hookOutputs.value = { ...hookOutputs.value, ...pendingOutputs };
        pendingOutputs = {};
      }
      if (pendingFeed.length) {
        embedFeed.value = [...pendingFeed.reverse(), ...embedFeed.value].slice(0, 100);
        pendingFeed = [];
      }
      if (pendingLogs.length) {
        logs.value = [...pendingLogs.reverse(), ...logs.value].slice(0, 200);
        pendingLogs = [];
      }
      if (pendingTraces.length) {
        debugTraces.value = [...pendingTraces.reverse(), ...debugTraces.value].slice(0, 300);
        pendingTraces = [];
      }
    });
  }

  on("hook:output", ({ key, text }) => {
    pendingOutputs[key] = text;
    scheduleFlush();
  });

  on("embed:line", (line) => {
    if (line && !line.cached && !line.error) {
      cacheCount.value++;
    }
    pendingFeed.push(line);
    scheduleFlush();
  });

  on("log", (entry) => {
    pendingLogs.push(entry);
    scheduleFlush();
  });

  on("debug:trace", (trace) => {
    pendingTraces.push(trace);
    scheduleFlush();
  });

  on("telemetry:batch", (batch) => {
    if (!batch) return;
    if (Array.isArray(batch.events) && batch.events.length) {
      const incoming = [...batch.events].reverse();
      telemetryEvents.value = [...incoming, ...telemetryEvents.value].slice(0, 2000);
    }
    if (Array.isArray(batch.stats)) {
      functionStats.value = batch.stats;
    }
    if (typeof batch.droppedEvents === "number") {
      droppedEvents.value = batch.droppedEvents;
    }
  });

  refreshAll();
}

async function refreshAll() {
  const [st, cfg, hk, ver] = await Promise.all([
    call("Status"),
    call("GetConfig"),
    call("Hooks"),
    call("AppVersion"),
  ]);
  if (st) status.value = st;
  if (cfg) config.value = cfg;
  if (hk) hooks.value = hk;
  if (ver) appVersion.value = ver;
  await Promise.all([refreshProcesses(), refreshSavedGames(), refreshUnityFontStatus(), refreshTelemetry()]);
}

async function refreshProcesses() {
  try {
    const list = await call("ListProcesses");
    if (list) processes.value = list;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function attach(pid) {
  errorBanner.value = "";
  try {
    hooks.value = [];
    hookOutputs.value = {};
    await call("Attach", Number(pid));
    const st = await call("Status");
    if (st) status.value = st;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function detach() {
  errorBanner.value = "";
  await call("Detach");
  hooks.value = [];
  hookOutputs.value = {};
}

async function setEmbed(key, enabled) {
  errorBanner.value = "";
  try {
    await call("SetEmbed", key, enabled);
    hooks.value = hooks.value.map((x) => (x.key === key ? { ...x, embedding: enabled } : x));
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function saveConfig(next) {
  errorBanner.value = "";
  saveState.value = "saving";
  try {
    await call("SaveConfig", next);
    config.value = next;
    saveState.value = "saved";
    setTimeout(() => {
      if (saveState.value === "saved") saveState.value = "";
    }, 1500);
  } catch (e) {
    saveState.value = "";
    errorBanner.value = String(e);
  }
}

const previewArabic = (text) => call("PreviewArabic", text);
const testTranslate = (text) => call("TestTranslate", text);
const testTranslateWithConfig = (text, cfg) => call("TestTranslateWithConfig", text, cfg);
const listFonts = () => call("ListFonts");
const startLlamaServer = (cfg) => call("StartLlamaServer", cfg);
const stopLlamaServer = () => call("StopLlamaServer");
const llamaServerStatus = () => call("LlamaServerStatus");
const browseLlamaBinary = () => call("BrowseLlamaBinary");
const browseGGUFModel = () => call("BrowseGGUFModel");
const setDebugMode = (enabled, realTrans = true) => call("SetDebugMode", Boolean(enabled), Boolean(realTrans));
const clearDebugTraces = () => {
  debugTraces.value = [];
};

async function refreshTelemetry() {
  try {
    const snap = await call("GetTelemetrySnapshot");
    if (!snap) return;
    if (Array.isArray(snap.events)) {
      telemetryEvents.value = [...snap.events].reverse().slice(0, 2000);
    }
    if (Array.isArray(snap.stats)) {
      functionStats.value = snap.stats;
    }
    if (typeof snap.droppedEvents === "number") {
      droppedEvents.value = snap.droppedEvents;
    }
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function clearTelemetry() {
  errorBanner.value = "";
  try {
    await call("ClearTelemetry");
    telemetryEvents.value = [];
    functionStats.value = [];
    droppedEvents.value = 0;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function exportDiagnosticBundle() {
  errorBanner.value = "";
  try {
    return await call("ExportDiagnosticBundle");
  } catch (e) {
    errorBanner.value = String(e);
    return "";
  }
}

async function refreshSavedGames() {
  try {
    const [games, count] = await Promise.all([call("SavedGames"), call("CacheCount")]);
    if (games) savedGames.value = games;
    if (typeof count === "number") cacheCount.value = count;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function quickAttach(exePath) {
  errorBanner.value = "";
  try {
    hooks.value = [];
    hookOutputs.value = {};
    await call("QuickAttach", exePath);
    const st = await call("Status");
    if (st) status.value = st;
    await refreshSavedGames();
    return true;
  } catch (e) {
    errorBanner.value = String(e);
    return false;
  }
}

async function deleteSavedGame(exePath) {
  errorBanner.value = "";
  try {
    await call("DeleteSavedGame", exePath);
    await refreshSavedGames();
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function clearSavedGameHooks(exePath) {
  errorBanner.value = "";
  try {
    await call("ClearSavedGameHooks", exePath);
    await refreshSavedGames();
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function removeSavedGameHook(exePath, h) {
  errorBanner.value = "";
  try {
    await call("RemoveSavedGameHook", exePath, h.hookCode, h.ctx, h.ctx2);
    await refreshSavedGames();
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function clearTranslationCache() {
  errorBanner.value = "";
  try {
    await call("ClearTranslationCache");
    const count = await call("CacheCount");
    cacheCount.value = typeof count === "number" ? count : 0;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

const listCacheEntries = (gamePath = "", query = "", manualOnly = false, limit = 200) =>
  call("ListCacheEntries", gamePath, query, Boolean(manualOnly), Number(limit) || 200);

async function saveCacheEntry(gameUid, provider, sourceText, translated) {
  errorBanner.value = "";
  try {
    await call("SaveCacheEntry", gameUid, provider, sourceText, translated);
    await refreshSavedGames();
  } catch (e) {
    errorBanner.value = String(e);
    throw e;
  }
}

async function deleteCacheEntry(gameUid, provider, sourceText) {
  errorBanner.value = "";
  try {
    await call("DeleteCacheEntry", gameUid, provider, sourceText);
    await refreshSavedGames();
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function clearScopeCache(gamePath = "", includeManual = false) {
  errorBanner.value = "";
  try {
    const n = await call("ClearScopeCache", gamePath, Boolean(includeManual));
    await refreshSavedGames();
    return n;
  } catch (e) {
    errorBanner.value = String(e);
    return 0;
  }
}

async function saveGamePipeline(exePath, pipeline) {
  errorBanner.value = "";
  saveState.value = "saving";
  try {
    await call("SaveGamePipeline", exePath, pipeline);
    await refreshSavedGames();
    saveState.value = "saved";
    setTimeout(() => {
      if (saveState.value === "saved") saveState.value = "";
    }, 1500);
  } catch (e) {
    saveState.value = "";
    errorBanner.value = String(e);
  }
}
const clearError = () => {
  errorBanner.value = "";
};

async function refreshUnityFontStatus() {
  const st = await call("UnityFontStatus");
  if (st) unityFontStatus.value = st;
  return st;
}

async function downloadUnityFonts() {
  errorBanner.value = "";
  try {
    await call("DownloadUnityFonts");
  } catch (e) {
    errorBanner.value = String(e);
  } finally {
    await refreshUnityFontStatus();
  }
}

export function useSession() {
  init();
  return {
    status: readonly(status),
    processes: readonly(processes),
    hooks: readonly(hooks),
    hookOutputs: readonly(hookOutputs),
    embedFeed: readonly(embedFeed),
    logs: readonly(logs),
    debugTraces: readonly(debugTraces),
    telemetryEvents: readonly(telemetryEvents),
    functionStats: readonly(functionStats),
    droppedEvents: readonly(droppedEvents),
    savedGames: readonly(savedGames),
    cacheCount: readonly(cacheCount),
    appVersion: readonly(appVersion),
    pipelineTarget,
    config,
    unityFontStatus: readonly(unityFontStatus),
    errorBanner: readonly(errorBanner),
    saveState: readonly(saveState),
    refreshProcesses,
    refreshSavedGames,
    attach,
    quickAttach,
    detach,
    setEmbed,
    saveConfig,
    saveGamePipeline,
    deleteSavedGame,
    clearSavedGameHooks,
    removeSavedGameHook,
    clearTranslationCache,
    listCacheEntries,
    saveCacheEntry,
    deleteCacheEntry,
    clearScopeCache,
    previewArabic,
    testTranslate,
    testTranslateWithConfig,
    listFonts,
    startLlamaServer,
    stopLlamaServer,
    llamaServerStatus,
    browseLlamaBinary,
    browseGGUFModel,
    refreshUnityFontStatus,
    downloadUnityFonts,
    setDebugMode,
    clearDebugTraces,
    refreshTelemetry,
    clearTelemetry,
    exportDiagnosticBundle,
    clearError,
  };
}
