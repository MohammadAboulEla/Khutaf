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
  fallbackProvider: "",
  paused: false,
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
  location: "",
  appDataDir: "",
  appDir: "",
  downloading: false,
  progress: 0,
  message: "",
  error: "",
});
const errorBanner = shallowRef("");
const saveState = shallowRef("");
const benchmarkItems = ref([]);
const benchmarkRunning = shallowRef(false);
const benchmarkRunId = shallowRef("");
const benchmarkTotal = shallowRef(0);
const isAdmin = shallowRef(false);
const quickHooksOpen = shallowRef(false);
const quickHooksStandalone = shallowRef(false);
const hotkeyToast = shallowRef(null);
let toastTimer = null;

let initialized = false;

function init() {
  if (initialized) return;
  initialized = true;

  on("session:status", (st) => {
    if (st) status.value = st;
  });

  on("config:updated", (cfg) => {
    if (cfg) config.value = cfg;
  });

  on("hooks:popup", (payload) => {
    const isOpen =
      typeof payload === "object" && payload !== null ? Boolean(payload.open) : Boolean(payload);
    const isStandalone =
      typeof payload === "object" && payload !== null ? Boolean(payload.standalone) : false;
    quickHooksOpen.value = isOpen;
    quickHooksStandalone.value = isOpen && isStandalone;
  });

  on("hotkey:toast", (ev) => {
    if (!ev?.label) return;
    hotkeyToast.value = ev;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      hotkeyToast.value = null;
      toastTimer = null;
    }, 2400);
  });

  on("unityfonts:progress", (st) => {
    if (st) unityFontStatus.value = st;
  });

  on("translator:bench:item", (item) => {
    if (!item || item.runId !== benchmarkRunId.value) return;
    const idx = benchmarkItems.value.findIndex((x) => x.providerId === item.providerId);
    if (idx >= 0) {
      benchmarkItems.value[idx] = item;
    } else {
      benchmarkItems.value = [...benchmarkItems.value, item];
    }
  });

  on("translator:bench:done", (summary) => {
    if (!summary || summary.runId !== benchmarkRunId.value) return;
    if (Array.isArray(summary.items)) {
      benchmarkItems.value = summary.items;
    }
    benchmarkTotal.value = summary.total || benchmarkTotal.value;
    benchmarkRunning.value = false;
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
    flushTimer = setTimeout(() => {
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
    }, 32);
  }

  on("hook:output", ({ key, text }) => {
    pendingOutputs[key] = text;
    scheduleFlush();
  });

  on("embed:line", (line) => {
    if (line && !line.cached && !line.error) {
      cacheCount.value++;
    }
    if (line?.key && line?.original) {
      pendingOutputs[line.key] = line.original;
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

function applyHooksSnapshot(hk) {
  if (!Array.isArray(hk)) return;
  hooks.value = hk;
  const next = { ...hookOutputs.value };
  let changed = false;
  for (const h of hk) {
    if (h?.key && h.lastText && next[h.key] !== h.lastText) {
      next[h.key] = h.lastText;
      changed = true;
    }
  }
  if (changed) hookOutputs.value = next;
}

async function refreshHooks() {
  const hk = await call("Hooks");
  applyHooksSnapshot(hk);
}

async function refreshAll() {
  const [st, cfg, hk, ver, adm] = await Promise.all([
    call("Status"),
    call("GetConfig"),
    call("Hooks"),
    call("AppVersion"),
    call("IsAdmin"),
  ]);
  if (st) status.value = st;
  if (cfg) config.value = cfg;
  applyHooksSnapshot(hk);
  if (ver) appVersion.value = ver;
  if (typeof adm === "boolean") isAdmin.value = adm;
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
const listAllFonts = () => call("ListAllFonts");
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

async function saveGameOverrides(exePath, embedOverride, translatorOverride) {
  errorBanner.value = "";
  saveState.value = "saving";
  try {
    await call("SaveGameOverrides", exePath, embedOverride, translatorOverride);
    const st = await call("Status");
    if (st) status.value = st;
    await refreshSavedGames();
    saveState.value = "saved";
    setTimeout(() => {
      if (saveState.value === "saved") saveState.value = "";
    }, 1500);
  } catch (e) {
    saveState.value = "";
    errorBanner.value = String(e);
    throw e;
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

async function downloadUnityFonts(location = "appdata") {
  errorBanner.value = "";
  const loc = typeof location === "string" && location ? location : "appdata";
  try {
    if (window.go?.main?.App?.DownloadUnityFontsTo) {
      await call("DownloadUnityFontsTo", loc);
    } else {
      await call("DownloadUnityFonts");
    }
  } catch (e) {
    errorBanner.value = String(e);
  } finally {
    await refreshUnityFontStatus();
  }
}

async function benchmarkTranslators(req) {
  errorBanner.value = "";
  const runId = `bench-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  benchmarkRunId.value = runId;
  benchmarkItems.value = [];
  const total =
    Array.isArray(req.providers) && req.providers.length
      ? req.providers.length
      : (req.includeTraditional ? 13 : 0) + (req.includeLlm ? 1 : 0) + (req.includeOfflineLlm ? 1 : 0);
  benchmarkTotal.value = total;
  benchmarkRunning.value = true;
  try {
    const sum = await call("BenchmarkTranslators", { ...req, runId });
    if (sum && sum.runId === benchmarkRunId.value && Array.isArray(sum.items)) {
      benchmarkItems.value = sum.items;
      benchmarkTotal.value = sum.total || total;
    }
    return sum;
  } catch (e) {
    errorBanner.value = String(e);
    return null;
  } finally {
    if (benchmarkRunId.value === runId) {
      benchmarkRunning.value = false;
    }
  }
}

async function cancelBenchmark() {
  await call("CancelBenchmark");
  benchmarkRunning.value = false;
}

async function smartAutoEmbed() {
  errorBanner.value = "";
  try {
    const res = await call("SmartAutoEmbed");
    const hk = await call("Hooks");
    if (hk) hooks.value = hk;
    await refreshSavedGames();
    return res;
  } catch (e) {
    errorBanner.value = String(e);
    throw e;
  }
}

async function restartAsAdmin() {
  errorBanner.value = "";
  try {
    await call("RestartAsAdmin");
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function togglePause() {
  errorBanner.value = "";
  try {
    await call("TogglePause");
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function setGameAutoAttach(exePath, enabled) {
  errorBanner.value = "";
  const prev = savedGames.value;
  savedGames.value = prev.map((g) => (g.exePath === exePath ? { ...g, autoAttach: Boolean(enabled) } : g));
  try {
    await call("SetGameAutoAttach", exePath, Boolean(enabled));
  } catch (e) {
    savedGames.value = prev;
    errorBanner.value = String(e);
  }
}

function hideToTray() {
  call("HideToTray");
}

function quitApp() {
  call("QuitApp");
}

function setQuickHooksOpen(open) {
  quickHooksOpen.value = Boolean(open);
  if (!open) {
    quickHooksStandalone.value = false;
  }
  call("SetHooksPopupOpen", Boolean(open));
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
    benchmarkItems: readonly(benchmarkItems),
    benchmarkRunning: readonly(benchmarkRunning),
    benchmarkTotal: readonly(benchmarkTotal),
    isAdmin: readonly(isAdmin),
    quickHooksOpen: readonly(quickHooksOpen),
    quickHooksStandalone: readonly(quickHooksStandalone),
    hotkeyToast: readonly(hotkeyToast),
    refreshHooks,
    refreshProcesses,
    refreshSavedGames,
    attach,
    quickAttach,
    detach,
    setEmbed,
    saveConfig,
    saveGamePipeline,
    saveGameOverrides,
    setGameAutoAttach,
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
    listAllFonts,
    startLlamaServer,
    stopLlamaServer,
    llamaServerStatus,
    browseLlamaBinary,
    browseGGUFModel,
    refreshUnityFontStatus,
    downloadUnityFonts,
    benchmarkTranslators,
    cancelBenchmark,
    smartAutoEmbed,
    restartAsAdmin,
    togglePause,
    hideToTray,
    quitApp,
    setQuickHooksOpen,
    setDebugMode,
    clearDebugTraces,
    refreshTelemetry,
    clearTelemetry,
    exportDiagnosticBundle,
    clearError,
  };
}
