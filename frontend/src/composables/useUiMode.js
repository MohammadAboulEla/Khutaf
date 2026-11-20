import { computed, ref, watchEffect } from "vue";

const STORAGE_KEY = "khutaf.uiMode";

const readStoredMode = () => {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved === "advanced" || saved === "simple" ? saved : "simple";
  } catch {
    return "simple";
  }
};

const uiMode = ref(readStoredMode());
const isSimple = computed(() => uiMode.value === "simple");

watchEffect(() => {
  try {
    localStorage.setItem(STORAGE_KEY, uiMode.value);
  } catch {
    // Ignore storage errors.
  }
});

function setUiMode(next) {
  uiMode.value = next === "advanced" ? "advanced" : "simple";
}

function toggleUiMode() {
  setUiMode(uiMode.value === "simple" ? "advanced" : "simple");
}

function syncFromConfig(uiCfg) {
  if (!uiCfg) return;
  if (uiCfg.mode === "simple" || uiCfg.mode === "advanced") {
    uiMode.value = uiCfg.mode;
  }
}

export function useUiMode() {
  return {
    uiMode,
    isSimple,
    setUiMode,
    toggleUiMode,
    syncFromConfig,
  };
}
