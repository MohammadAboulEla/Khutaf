import { shallowRef } from "vue";

export const errorBanner = shallowRef("");
export const saveState = shallowRef("");

export function clearError() {
  errorBanner.value = "";
}

export function setError(e) {
  errorBanner.value = String(e);
}

export function markSaving() {
  saveState.value = "saving";
}

export function markSaved() {
  saveState.value = "saved";
  setTimeout(() => {
    if (saveState.value === "saved") saveState.value = "";
  }, 1500);
}

export function resetSaveState() {
  saveState.value = "";
}
