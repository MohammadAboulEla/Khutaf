import { ref, shallowRef } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, setError } from "./sessionState.js";

const MAX_HISTORY_PER_HOOK = 15;

export const hooks = ref([]);
export const hookOutputs = ref({});
export const hookHistory = ref({});
export const hookPulses = ref({});
export const embedFeed = ref([]);
export const quickHooksOpen = shallowRef(false);
export const quickHooksStandalone = shallowRef(false);

export function resetHookState() {
  hooks.value = [];
  hookOutputs.value = {};
  hookHistory.value = {};
  hookPulses.value = {};
}

function mergeHistoryList(existing, text, atMs) {
  const clean = String(text || "").trim();
  if (!clean) return existing || [];
  const prev = Array.isArray(existing) ? existing : [];
  const filtered = prev.filter((item) => item.text !== clean);
  return [{ text: clean, atMs: atMs || Date.now() }, ...filtered].slice(0, MAX_HISTORY_PER_HOOK);
}

export function applyHooksSnapshot(hk) {
  if (!Array.isArray(hk)) return;
  hooks.value = hk;
  const nextOut = { ...hookOutputs.value };
  const nextHist = { ...hookHistory.value };
  let outChanged = false;
  let histChanged = false;

  for (const h of hk) {
    if (!h?.key) continue;
    if (h.lastText && nextOut[h.key] !== h.lastText) {
      nextOut[h.key] = h.lastText;
      outChanged = true;
    }
    if (Array.isArray(h.history) && h.history.length) {
      nextHist[h.key] = h.history.slice(0, MAX_HISTORY_PER_HOOK);
      histChanged = true;
    } else if (h.lastText && !nextHist[h.key]?.length) {
      nextHist[h.key] = [{ text: h.lastText, atMs: h.lastSeenMs || Date.now() }];
      histChanged = true;
    }
  }
  if (outChanged) hookOutputs.value = nextOut;
  if (histChanged) hookHistory.value = nextHist;
}

export function applyHookOutputBatch(pendingOutputs) {
  const entries = Object.entries(pendingOutputs || {});
  if (!entries.length) return;

  const nextOut = { ...hookOutputs.value };
  const nextHist = { ...hookHistory.value };
  const nextPulses = { ...hookPulses.value };
  let metaDirty = false;
  const metaUpdates = new Map();

  for (const [key, payload] of entries) {
    const isObj = typeof payload === "object" && payload !== null;
    const text = isObj ? payload.text : payload;
    const atMs = (isObj && payload.atMs) || Date.now();
    if (typeof text === "string" && text.trim()) {
      nextOut[key] = text;
      nextHist[key] = mergeHistoryList(nextHist[key], text, atMs);
      nextPulses[key] = atMs;
    }
    if (isObj && (payload.category || payload.hitCount || payload.objectPath)) {
      metaUpdates.set(key, payload);
      metaDirty = true;
    }
  }

  hookOutputs.value = nextOut;
  hookHistory.value = nextHist;
  hookPulses.value = nextPulses;

  if (metaDirty && hooks.value.length) {
    let changed = false;
    const updated = hooks.value.map((h) => {
      const m = metaUpdates.get(h.key);
      if (!m) return h;
      changed = true;
      return {
        ...h,
        lastText: m.text || h.lastText,
        category: m.category || h.category,
        dialogueScore: typeof m.dialogueScore === "number" ? m.dialogueScore : h.dialogueScore,
        hitCount: typeof m.hitCount === "number" ? m.hitCount : h.hitCount,
        updatesPerSec: typeof m.updatesPerSec === "number" ? m.updatesPerSec : h.updatesPerSec,
        lastSeenMs: typeof m.lastSeenMs === "number" ? m.lastSeenMs : h.lastSeenMs,
        objectPath: m.objectPath || h.objectPath,
      };
    });
    if (changed) hooks.value = updated;
  }
}

export function registerHookListeners({ enqueueOutput, enqueueEmbedLine }) {
  onEvent("hooks:popup", (payload) => {
    const isOpen =
      typeof payload === "object" && payload !== null ? Boolean(payload.open) : Boolean(payload);
    const isStandalone =
      typeof payload === "object" && payload !== null ? Boolean(payload.standalone) : false;
    quickHooksOpen.value = isOpen;
    quickHooksStandalone.value = isOpen && isStandalone;
  });

  onEvent("hook:new", (h) => {
    if (!h?.key) return;
    const idx = hooks.value.findIndex((x) => x.key === h.key);
    if (idx >= 0) {
      const copy = [...hooks.value];
      copy[idx] = h;
      hooks.value = copy;
    } else {
      hooks.value = [...hooks.value, h];
    }
    if (Array.isArray(h.history) && h.history.length) {
      hookHistory.value = { ...hookHistory.value, [h.key]: h.history.slice(0, MAX_HISTORY_PER_HOOK) };
    }
    if (h.lastText) {
      hookOutputs.value = { ...hookOutputs.value, [h.key]: h.lastText };
    }
  });

  onEvent("hook:remove", (key) => {
    hooks.value = hooks.value.filter((x) => x.key !== key);
  });

  onEvent("hook:output", enqueueOutput);
  onEvent("embed:line", enqueueEmbedLine);
}

export async function refreshHooks() {
  const hk = await callApp("Hooks");
  applyHooksSnapshot(hk);
}

export async function setEmbed(key, enabled) {
  clearError();
  try {
    await callApp("SetEmbed", key, enabled);
    hooks.value = hooks.value.map((x) =>
      x.key === key ? { ...x, embedding: enabled, muted: enabled ? false : x.muted } : x,
    );
  } catch (e) {
    setError(e);
  }
}

export async function setHookMuted(key, muted, refreshSavedGames) {
  clearError();
  try {
    await callApp("SetHookMuted", key, Boolean(muted));
    hooks.value = hooks.value.map((x) =>
      x.key === key
        ? { ...x, muted: Boolean(muted), embedding: muted ? false : x.embedding }
        : x,
    );
    if (refreshSavedGames) await refreshSavedGames();
  } catch (e) {
    setError(e);
  }
}

export async function applyHookPreset(preset, refreshSavedGames) {
  clearError();
  try {
    const res = await callApp("ApplyHookPreset", preset);
    await refreshHooks();
    if (refreshSavedGames) await refreshSavedGames();
    return res;
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function restoreEmbedHooks(keys, refreshSavedGames) {
  clearError();
  try {
    const res = await callApp("RestoreEmbedHooks", Array.isArray(keys) ? keys : []);
    await refreshHooks();
    if (refreshSavedGames) await refreshSavedGames();
    return res;
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function smartAutoEmbed(refreshSavedGames) {
  clearError();
  try {
    const res = await callApp("SmartAutoEmbed");
    await refreshHooks();
    if (refreshSavedGames) await refreshSavedGames();
    return res;
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function addGlossaryTerm(source, target, caseSensitive = false, scopeGame = true, refreshSavedGames) {
  clearError();
  try {
    await callApp("AddGlossaryTerm", source, target, Boolean(caseSensitive), Boolean(scopeGame));
    if (refreshSavedGames) await refreshSavedGames();
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function cycleDisplayMode() {
  clearError();
  try {
    return await callApp("CycleDisplayMode");
  } catch (e) {
    setError(e);
    return undefined;
  }
}

export async function adjustFontScale(delta) {
  clearError();
  try {
    return await callApp("AdjustFontScale", Number(delta));
  } catch (e) {
    setError(e);
    return undefined;
  }
}

export function setQuickHooksOpen(open) {
  quickHooksOpen.value = Boolean(open);
  if (!open) {
    quickHooksStandalone.value = false;
  }
  callApp("SetHooksPopupOpen", Boolean(open));
}

export function showMainFromHooksPopup() {
  quickHooksOpen.value = false;
  quickHooksStandalone.value = false;
  callApp("ShowMainFromHooksPopup");
}
