import { ref, shallowRef } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, setError } from "./sessionState.js";

export const hooks = ref([]);
export const hookOutputs = ref({});
export const embedFeed = ref([]);
export const quickHooksOpen = shallowRef(false);
export const quickHooksStandalone = shallowRef(false);

export function applyHooksSnapshot(hk) {
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
    const idx = hooks.value.findIndex((x) => x.key === h.key);
    if (idx >= 0) hooks.value[idx] = h;
    else hooks.value.push(h);
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
    hooks.value = hooks.value.map((x) => (x.key === key ? { ...x, embedding: enabled } : x));
  } catch (e) {
    setError(e);
  }
}

export async function smartAutoEmbed(refreshSavedGames) {
  clearError();
  try {
    const res = await callApp("SmartAutoEmbed");
    const hk = await callApp("Hooks");
    if (hk) hooks.value = hk;
    if (refreshSavedGames) await refreshSavedGames();
    return res;
  } catch (e) {
    setError(e);
    throw e;
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
