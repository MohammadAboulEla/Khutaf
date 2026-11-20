import { computed, ref, watchEffect } from "vue";
import { MESSAGES } from "../i18n/messages.js";

const STORAGE_KEY = "khutaf.lang";

const readStoredLang = () => {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved === "en" || saved === "ar" ? saved : "ar";
  } catch {
    return "ar";
  }
};

const lang = ref(readStoredLang());
const isRtl = computed(() => lang.value === "ar");

watchEffect(() => {
  const dir = isRtl.value ? "rtl" : "ltr";
  if (typeof document !== "undefined") {
    document.documentElement.dir = dir;
    document.documentElement.lang = lang.value;
  }
  try {
    localStorage.setItem(STORAGE_KEY, lang.value);
  } catch {
    // Ignore storage errors in restricted webviews.
  }
});

function t(key, vars = null) {
  const dict = MESSAGES[lang.value] || MESSAGES.ar;
  let str = dict[key] ?? MESSAGES.en[key] ?? key;
  if (vars && typeof vars === "object") {
    for (const [k, v] of Object.entries(vars)) {
      str = str.split(`{${k}}`).join(String(v ?? ""));
    }
  }
  return str;
}

function setLang(next) {
  lang.value = next === "en" ? "en" : "ar";
}

function toggleLang() {
  setLang(lang.value === "ar" ? "en" : "ar");
}

function syncFromConfig(uiCfg) {
  if (!uiCfg) return;
  if (uiCfg.language === "ar" || uiCfg.language === "en") {
    lang.value = uiCfg.language;
  }
}

export function useI18n() {
  return {
    lang,
    isRtl,
    t,
    setLang,
    toggleLang,
    syncFromConfig,
  };
}
