import { ref, shallowRef, readonly } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, errorBanner, saveState, setError } from "./sessionState.js";
import {
  registerUpdaterListeners,
  updateStatus,
  checkForUpdates,
  downloadUpdate,
  cancelUpdateDownload,
  installUpdate,
  skipUpdateVersion,
  openReleasePage,
} from "./useUpdater.js";
import {
  registerUnityFontListeners,
  unityFontStatus,
  fontDownloadPromptOpen,
  refreshUnityFontStatus,
  openFontDownloadPrompt,
  closeFontDownloadPrompt,
  downloadUnityFonts,
} from "./useUnityFonts.js";
import {
  registerBenchmarkListeners,
  benchmarkItems,
  benchmarkRunning,
  benchmarkTotal,
  benchmarkTranslators,
  cancelBenchmark,
} from "./useBenchmark.js";
import {
  registerTelemetryListeners,
  logs,
  debugTraces,
  telemetryEvents,
  functionStats,
  droppedEvents,
  setDebugMode,
  clearDebugTraces,
  refreshTelemetry,
  clearTelemetry,
  exportDiagnosticBundle,
} from "./useTelemetry.js";
import {
  registerHookListeners,
  hooks,
  hookOutputs,
  embedFeed,
  quickHooksOpen,
  quickHooksStandalone,
  applyHooksSnapshot,
  refreshHooks,
  setEmbed,
  smartAutoEmbed as runSmartAutoEmbed,
  setQuickHooksOpen,
  showMainFromHooksPopup,
} from "./useHooks.js";
import {
  registerConfigListeners,
  config,
  savedGames,
  cacheCount,
  pipelineTarget,
  saveConfig,
  refreshSavedGames,
  deleteSavedGame,
  clearSavedGameHooks,
  removeSavedGameHook,
  clearTranslationCache,
  listCacheEntries,
  saveCacheEntry,
  deleteCacheEntry,
  clearScopeCache,
  saveGamePipeline,
  saveGameOverrides as saveOverrides,
  setGameAutoAttach,
} from "./useConfig.js";

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
const appVersion = ref("");
const isAdmin = shallowRef(false);
const hotkeyToast = shallowRef(null);
let toastTimer = null;
let initialized = false;

function init() {
  if (initialized) return;
  initialized = true;

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

  onEvent("session:status", (st) => {
    if (st) status.value = st;
  });

  onEvent("hotkey:toast", (ev) => {
    if (!ev?.label) return;
    hotkeyToast.value = ev;
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      hotkeyToast.value = null;
      toastTimer = null;
    }, 2400);
  });

  registerConfigListeners();
  registerUnityFontListeners();
  registerUpdaterListeners();
  registerBenchmarkListeners();
  registerHookListeners({
    enqueueOutput: ({ key, text }) => {
      pendingOutputs[key] = text;
      scheduleFlush();
    },
    enqueueEmbedLine: (line) => {
      if (line && !line.cached && !line.error) {
        cacheCount.value++;
      }
      if (line?.key && line?.original) {
        pendingOutputs[line.key] = line.original;
      }
      pendingFeed.push(line);
      scheduleFlush();
    },
  });
  registerTelemetryListeners({
    enqueueLog: (entry) => {
      pendingLogs.push(entry);
      scheduleFlush();
    },
    enqueueTrace: (trace) => {
      pendingTraces.push(trace);
      scheduleFlush();
    },
  });

  refreshAll();
}

async function refreshAll() {
  const [st, cfg, hk, ver, adm, upd] = await Promise.all([
    callApp("Status"),
    callApp("GetConfig"),
    callApp("Hooks"),
    callApp("AppVersion"),
    callApp("IsAdmin"),
    callApp("GetUpdateStatus"),
  ]);
  if (st) status.value = st;
  if (cfg) config.value = cfg;
  applyHooksSnapshot(hk);
  if (ver) appVersion.value = ver;
  if (typeof adm === "boolean") isAdmin.value = adm;
  if (upd) updateStatus.value = upd;
  await Promise.all([refreshProcesses(), refreshSavedGames(), refreshUnityFontStatus(), refreshTelemetry()]);
}

async function refreshProcesses() {
  try {
    const list = await callApp("ListProcesses");
    if (list) processes.value = list;
  } catch (e) {
    setError(e);
  }
}

async function attach(pid) {
  clearError();
  try {
    hooks.value = [];
    hookOutputs.value = {};
    await callApp("Attach", Number(pid));
    const st = await callApp("Status");
    if (st) status.value = st;
  } catch (e) {
    setError(e);
  }
}

async function quickAttach(exePath) {
  clearError();
  try {
    hooks.value = [];
    hookOutputs.value = {};
    await callApp("QuickAttach", exePath);
    const st = await callApp("Status");
    if (st) status.value = st;
    await refreshSavedGames();
    return true;
  } catch (e) {
    setError(e);
    return false;
  }
}

async function detach() {
  clearError();
  await callApp("Detach");
  hooks.value = [];
  hookOutputs.value = {};
}

const previewArabic = (text) => callApp("PreviewArabic", text);
const testTranslate = (text) => callApp("TestTranslate", text);
const testTranslateWithConfig = (text, cfg) => callApp("TestTranslateWithConfig", text, cfg);
const listFonts = () => callApp("ListFonts");
const listAllFonts = () => callApp("ListAllFonts");
const startLlamaServer = (cfg) => callApp("StartLlamaServer", cfg);
const stopLlamaServer = () => callApp("StopLlamaServer");
const llamaServerStatus = () => callApp("LlamaServerStatus");
const browseLlamaBinary = () => callApp("BrowseLlamaBinary");
const browseGGUFModel = () => callApp("BrowseGGUFModel");

const saveGameOverrides = (exePath, embedOverride, translatorOverride) =>
  saveOverrides(exePath, embedOverride, translatorOverride, (st) => {
    status.value = st;
  });

const smartAutoEmbed = () => runSmartAutoEmbed(refreshSavedGames);

async function restartAsAdmin() {
  clearError();
  try {
    await callApp("RestartAsAdmin");
  } catch (e) {
    setError(e);
  }
}

async function togglePause() {
  clearError();
  try {
    await callApp("TogglePause");
  } catch (e) {
    setError(e);
  }
}

function hideToTray() {
  callApp("HideToTray");
}

function quitApp() {
  callApp("QuitApp");
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
    updateStatus: readonly(updateStatus),
    errorBanner: readonly(errorBanner),
    saveState: readonly(saveState),
    benchmarkItems: readonly(benchmarkItems),
    benchmarkRunning: readonly(benchmarkRunning),
    benchmarkTotal: readonly(benchmarkTotal),
    isAdmin: readonly(isAdmin),
    quickHooksOpen: readonly(quickHooksOpen),
    quickHooksStandalone: readonly(quickHooksStandalone),
    hotkeyToast: readonly(hotkeyToast),
    fontDownloadPromptOpen: readonly(fontDownloadPromptOpen),
    openFontDownloadPrompt,
    closeFontDownloadPrompt,
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
    showMainFromHooksPopup,
    setDebugMode,
    clearDebugTraces,
    refreshTelemetry,
    clearTelemetry,
    exportDiagnosticBundle,
    clearError,
    checkForUpdates,
    downloadUpdate,
    cancelUpdateDownload,
    installUpdate,
    skipUpdateVersion,
    openReleasePage,
  };
}
