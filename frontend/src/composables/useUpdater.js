import { ref } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";

export const updateStatus = ref({
  state: "idle",
  currentVersion: "",
  latestVersion: "",
  tagName: "",
  releaseName: "",
  releaseNotes: "",
  releaseUrl: "",
  publishedAt: "",
  assetName: "",
  assetUrl: "",
  assetSize: 0,
  assetSha256: "",
  progress: 0,
  downloadedBytes: 0,
  totalBytes: 0,
  lastCheckedAt: "",
  writable: true,
  prerelease: false,
  error: "",
  updatedFrom: "",
});

export function registerUpdaterListeners() {
  onEvent("updater:status", (st) => {
    if (st) updateStatus.value = st;
  });
}

export async function checkForUpdates(manual = true) {
  try {
    const st = await callApp("CheckForUpdates", Boolean(manual));
    if (st) updateStatus.value = st;
    return st;
  } catch (e) {
    const cur = await callApp("GetUpdateStatus");
    if (cur) updateStatus.value = cur;
    throw e;
  }
}

export function downloadUpdate() {
  return callApp("DownloadUpdate");
}

export function cancelUpdateDownload() {
  return callApp("CancelUpdateDownload");
}

export function installUpdate(detachIfAttached = true) {
  return callApp("InstallUpdate", Boolean(detachIfAttached));
}

export function skipUpdateVersion(version) {
  return callApp("SkipUpdateVersion", version || "");
}

export function openReleasePage(url) {
  return callApp("OpenReleasePage", url || "");
}
