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
  debugRealTrans: false,
});

const processes = ref([]);
const hooks = ref([]);
const hookOutputs = ref({});
const embedFeed = ref([]);
const logs = ref([]);
const debugTraces = ref([]);
const config = ref(null);
const errorBanner = shallowRef("");
const saveState = shallowRef("");

let initialized = false;

function init() {
  if (initialized) return;
  initialized = true;

  on("session:status", (st) => {
    if (st) status.value = st;
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

  refreshAll();
}

async function refreshAll() {
  const [st, cfg, hk] = await Promise.all([
    call("Status"),
    call("GetConfig"),
    call("Hooks"),
  ]);
  if (st) status.value = st;
  if (cfg) config.value = cfg;
  if (hk) hooks.value = hk;
  await refreshProcesses();
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
    const h = hooks.value.find((x) => x.key === key);
    if (h) h.embedding = enabled;
  } catch (e) {
    errorBanner.value = String(e);
  }
}

async function insertHookCode(code) {
  if (!code?.trim()) return;
  errorBanner.value = "";
  try {
    await call("InsertHookCode", code.trim());
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
const listFonts = () => call("ListFonts");
const setDebugMode = (enabled, realTrans = false) => call("SetDebugMode", Boolean(enabled), Boolean(realTrans));
const clearDebugTraces = () => {
  debugTraces.value = [];
};
const clearError = () => {
  errorBanner.value = "";
};

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
    config,
    errorBanner: readonly(errorBanner),
    saveState: readonly(saveState),
    refreshProcesses,
    attach,
    detach,
    setEmbed,
    insertHookCode,
    saveConfig,
    previewArabic,
    testTranslate,
    listFonts,
    setDebugMode,
    clearDebugTraces,
    clearError,
  };
}
