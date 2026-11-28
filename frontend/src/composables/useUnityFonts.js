import { ref, shallowRef } from "vue";
import { callApp, hasAppMethod, onEvent } from "./wailsBridge.js";
import { clearError, setError } from "./sessionState.js";

export const unityFontStatus = ref({
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

export const fontDownloadPromptOpen = shallowRef(false);

export function registerUnityFontListeners() {
  onEvent("unityfonts:progress", (st) => {
    if (st) unityFontStatus.value = st;
  });
}

export async function refreshUnityFontStatus() {
  const st = await callApp("UnityFontStatus");
  if (st) unityFontStatus.value = st;
  return st;
}

export function openFontDownloadPrompt() {
  refreshUnityFontStatus();
  fontDownloadPromptOpen.value = true;
}

export function closeFontDownloadPrompt() {
  fontDownloadPromptOpen.value = false;
}

export async function downloadUnityFonts(location = "appdata") {
  clearError();
  const loc = typeof location === "string" && location ? location : "appdata";
  try {
    if (hasAppMethod("DownloadUnityFontsTo")) {
      await callApp("DownloadUnityFontsTo", loc);
    } else {
      await callApp("DownloadUnityFonts");
    }
  } catch (e) {
    setError(e);
  } finally {
    await refreshUnityFontStatus();
  }
}
