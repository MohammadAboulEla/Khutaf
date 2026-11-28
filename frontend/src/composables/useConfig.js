import { ref } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, markSaved, markSaving, resetSaveState, setError } from "./sessionState.js";

export const config = ref(null);
export const savedGames = ref([]);
export const cacheCount = ref(0);
export const pipelineTarget = ref("global");

export function registerConfigListeners() {
  onEvent("config:updated", (cfg) => {
    if (cfg) config.value = cfg;
  });
}

export async function saveConfig(next) {
  clearError();
  markSaving();
  try {
    await callApp("SaveConfig", next);
    config.value = next;
    markSaved();
  } catch (e) {
    resetSaveState();
    setError(e);
  }
}

export async function refreshSavedGames() {
  try {
    const [games, count] = await Promise.all([callApp("SavedGames"), callApp("CacheCount")]);
    if (games) savedGames.value = games;
    if (typeof count === "number") cacheCount.value = count;
  } catch (e) {
    setError(e);
  }
}

export async function deleteSavedGame(exePath) {
  clearError();
  try {
    await callApp("DeleteSavedGame", exePath);
    await refreshSavedGames();
  } catch (e) {
    setError(e);
  }
}

export async function clearSavedGameHooks(exePath) {
  clearError();
  try {
    await callApp("ClearSavedGameHooks", exePath);
    await refreshSavedGames();
  } catch (e) {
    setError(e);
  }
}

export async function removeSavedGameHook(exePath, h) {
  clearError();
  try {
    await callApp("RemoveSavedGameHook", exePath, h.hookCode, h.ctx, h.ctx2);
    await refreshSavedGames();
  } catch (e) {
    setError(e);
  }
}

export async function clearTranslationCache() {
  clearError();
  try {
    await callApp("ClearTranslationCache");
    const count = await callApp("CacheCount");
    cacheCount.value = typeof count === "number" ? count : 0;
  } catch (e) {
    setError(e);
  }
}

export function listCacheEntries(gamePath = "", query = "", manualOnly = false, limit = 200) {
  return callApp("ListCacheEntries", gamePath, query, Boolean(manualOnly), Number(limit) || 200);
}

export async function saveCacheEntry(gameUid, provider, sourceText, translated) {
  clearError();
  try {
    await callApp("SaveCacheEntry", gameUid, provider, sourceText, translated);
    await refreshSavedGames();
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function deleteCacheEntry(gameUid, provider, sourceText) {
  clearError();
  try {
    await callApp("DeleteCacheEntry", gameUid, provider, sourceText);
    await refreshSavedGames();
  } catch (e) {
    setError(e);
  }
}

export async function clearScopeCache(gamePath = "", includeManual = false) {
  clearError();
  try {
    const n = await callApp("ClearScopeCache", gamePath, Boolean(includeManual));
    await refreshSavedGames();
    return n;
  } catch (e) {
    setError(e);
    return 0;
  }
}

export async function saveGamePipeline(exePath, pipeline) {
  clearError();
  markSaving();
  try {
    await callApp("SaveGamePipeline", exePath, pipeline);
    await refreshSavedGames();
    markSaved();
  } catch (e) {
    resetSaveState();
    setError(e);
  }
}

export async function saveGameOverrides(exePath, embedOverride, translatorOverride, onStatus) {
  clearError();
  markSaving();
  try {
    await callApp("SaveGameOverrides", exePath, embedOverride, translatorOverride);
    const st = await callApp("Status");
    if (st && onStatus) onStatus(st);
    await refreshSavedGames();
    markSaved();
  } catch (e) {
    resetSaveState();
    setError(e);
    throw e;
  }
}

export async function setGameAutoAttach(exePath, enabled) {
  clearError();
  const prev = savedGames.value;
  savedGames.value = prev.map((g) => (g.exePath === exePath ? { ...g, autoAttach: Boolean(enabled) } : g));
  try {
    await callApp("SetGameAutoAttach", exePath, Boolean(enabled));
  } catch (e) {
    savedGames.value = prev;
    setError(e);
  }
}

export async function exportGameProfile(exePath) {
  clearError();
  try {
    return await callApp("ExportGameProfile", exePath);
  } catch (e) {
    setError(e);
    throw e;
  }
}

export async function importGameProfile(exePath) {
  clearError();
  try {
    const res = await callApp("ImportGameProfile", exePath);
    if (res && (res.title || res.hooksAdded || res.mutedAdded || res.glossaryAdded || res.rulesAdded || res.embedOverrideApplied)) {
      await refreshSavedGames();
    }
    return res;
  } catch (e) {
    setError(e);
    throw e;
  }
}
