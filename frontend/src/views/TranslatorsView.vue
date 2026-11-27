<script setup>
import { computed, onMounted, onUnmounted, reactive, shallowRef, watch } from "vue";
import Icon from "../components/Icon.vue";
import TranslatorBenchmark from "../components/TranslatorBenchmark.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { useUiMode } from "../composables/useUiMode.js";
import { ALL_PROVIDERS, TRADITIONAL_PROVIDERS } from "../utils/providers.js";

const {
  config,
  saveConfig,
  saveState,
  testTranslateWithConfig,
  startLlamaServer,
  stopLlamaServer,
  llamaServerStatus,
  browseLlamaBinary,
  browseGGUFModel,
} = useSession();

const { t } = useI18n();
const { isSimple } = useUiMode();

const PRESETS = [
  { name: "OpenAI", url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  { name: "Gemini (OpenAI compat)", url: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-2.5-flash" },
  { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat" },
  { name: "Groq", url: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile" },
  { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "google/gemini-2.5-flash" },
  { name: "Local Ollama", url: "http://127.0.0.1:11434/v1", model: "qwen2.5:7b" },
  { name: "Local llama.cpp / LM Studio", url: "http://127.0.0.1:8080/v1", model: "local-model" },
];

const OFFLINE_TEMPLATES = [
  { id: "auto", label: "Auto-detect from model name" },
  { id: "hymt2", label: "HY-MT2 / Hunyuan-MT" },
  { id: "translate_gemma", label: "TranslateGemma" },
  { id: "nllb_instruct", label: "Qwen-MT / Instruction MT" },
  { id: "generic_local", label: "Generic Local Game Translator" },
];

const form = reactive({
  active: "bing",
  fallbackProvider: "",
  bingSourceLang: "auto-detect",
  bingTargetLang: "ar",
  tradSourceLang: "auto",
  tradTargetLang: "ar",
  baseUrl: "",
  apiKey: "",
  model: "",
  temperature: 0.2,
  sourceLang: "English",
  targetLang: "Arabic",
  systemPrompt: "",
  offBaseUrl: "http://127.0.0.1:8080/v1",
  offModel: "local-model",
  offTemplate: "auto",
  offSourceLang: "English",
  offTargetLang: "Arabic",
  offTemperature: 0.1,
  llamaBinaryPath: "",
  llamaModelPath: "",
  llamaPort: 8080,
  llamaGpuLayers: 99,
  llamaCtxSize: 4096,
  llamaAutoStart: false,
  llamaExtraArgs: "",
});

function providerDisplayName(id) {
  if (!id || id === "none") return t("trDisabled");
  if (id === "offline_llm") return "Offline LLM (GGUF / llama.cpp)";
  if (id === "llm") return "OpenAI-Compatible LLM";
  return ALL_PROVIDERS.find((p) => p.id === id)?.label || id;
}

const isTraditional = computed(() =>
  TRADITIONAL_PROVIDERS.some((p) => p.id === form.active),
);
const activeLabel = computed(() => providerDisplayName(form.active));
const fallbackLabel = computed(() =>
  form.fallbackProvider && form.fallbackProvider !== form.active
    ? providerDisplayName(form.fallbackProvider)
    : "",
);

const testInput = shallowRef("[Test] Welcome, Hero! If you can read this in Arabic, your translator is working.");
const testResult = shallowRef(null);
const testErr = shallowRef("");
const testing = shallowRef(false);

const llamaStatus = shallowRef({ running: false, ready: false, pid: 0, port: 8080, recentLogs: "", lastError: "" });
const llamaBusy = shallowRef(false);
const llamaErr = shallowRef("");
const showLlamaLogs = shallowRef(false);
let llamaTimer = null;

async function refreshLlamaStatus() {
  const st = await llamaServerStatus();
  if (st) llamaStatus.value = st;
}

function normalizeTranslator(tr) {
  const src = tr || {};
  const active = src.active || "bing";
  const fb = src.fallbackProvider === active ? "" : src.fallbackProvider || "";
  const llm = src.llm || {};
  const off = src.offlineLlm || {};
  const lc = src.llamaCpp || {};
  return {
    active,
    fallbackProvider: fb,
    bing: {
      sourceLang: (src.bing?.sourceLang || "auto-detect").trim() || "auto-detect",
      targetLang: (src.bing?.targetLang || "ar").trim() || "ar",
    },
    traditional: {
      sourceLang: (src.traditional?.sourceLang || "auto").trim() || "auto",
      targetLang: (src.traditional?.targetLang || "ar").trim() || "ar",
    },
    llm: {
      baseUrl: (llm.baseUrl || "").trim(),
      apiKey: (llm.apiKey || "").trim(),
      model: (llm.model || "").trim(),
      temperature: Number(llm.temperature) || 0.2,
      sourceLang: (llm.sourceLang || "English").trim() || "English",
      targetLang: (llm.targetLang || "Arabic").trim() || "Arabic",
      systemPrompt: llm.systemPrompt || "",
    },
    offlineLlm: {
      baseUrl: (off.baseUrl || "http://127.0.0.1:8080/v1").trim() || "http://127.0.0.1:8080/v1",
      model: (off.model || "local-model").trim() || "local-model",
      template: off.template || "auto",
      sourceLang: (off.sourceLang || "English").trim() || "English",
      targetLang: (off.targetLang || "Arabic").trim() || "Arabic",
      temperature: Number(off.temperature) || 0.1,
    },
    llamaCpp: {
      binaryPath: (lc.binaryPath || "").trim(),
      modelPath: (lc.modelPath || "").trim(),
      port: Number(lc.port) || 8080,
      gpuLayers: Number(lc.gpuLayers) ?? 99,
      ctxSize: Number(lc.ctxSize) || 4096,
      autoStart: Boolean(lc.autoStart),
      extraArgs: (lc.extraArgs || "").trim(),
    },
  };
}

function applyConfigToForm(cfg) {
  if (!cfg?.translator) return;
  form.active = cfg.translator.active || "bing";
  form.fallbackProvider = cfg.translator.fallbackProvider || "";
  form.bingSourceLang = cfg.translator.bing?.sourceLang || "auto-detect";
  form.bingTargetLang = cfg.translator.bing?.targetLang || "ar";
  form.tradSourceLang = cfg.translator.traditional?.sourceLang || "auto";
  form.tradTargetLang = cfg.translator.traditional?.targetLang || "ar";
  Object.assign(form, cfg.translator.llm);
  const off = cfg.translator.offlineLlm;
  if (off) {
    form.offBaseUrl = off.baseUrl || "http://127.0.0.1:8080/v1";
    form.offModel = off.model || "local-model";
    form.offTemplate = off.template || "auto";
    form.offSourceLang = off.sourceLang || "English";
    form.offTargetLang = off.targetLang || "Arabic";
    form.offTemperature = off.temperature ?? 0.1;
  }
  const lc = cfg.translator.llamaCpp;
  if (lc) {
    form.llamaBinaryPath = lc.binaryPath || "";
    form.llamaModelPath = lc.modelPath || "";
    form.llamaPort = lc.port || 8080;
    form.llamaGpuLayers = lc.gpuLayers ?? 99;
    form.llamaCtxSize = lc.ctxSize || 4096;
    form.llamaAutoStart = Boolean(lc.autoStart);
    form.llamaExtraArgs = lc.extraArgs || "";
  }
}

function onKeyDown(e) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    e.preventDefault();
    if (isDirty.value) onSave();
  }
}

onMounted(() => {
  window.addEventListener("keydown", onKeyDown);
  refreshLlamaStatus();
  llamaTimer = setInterval(refreshLlamaStatus, 3000);
});

onUnmounted(() => {
  window.removeEventListener("keydown", onKeyDown);
  clearInterval(llamaTimer);
});

watch(
  config,
  (cfg) => applyConfigToForm(cfg),
  { immediate: true },
);

function applyPreset(p) {
  form.active = "llm";
  form.baseUrl = p.url;
  form.model = p.model;
}

function buildTranslatorConfig() {
  return {
    active: form.active,
    fallbackProvider: form.fallbackProvider === form.active ? "" : form.fallbackProvider,
    bing: {
      sourceLang: form.bingSourceLang.trim() || "auto-detect",
      targetLang: form.bingTargetLang.trim() || "ar",
    },
    traditional: {
      sourceLang: form.tradSourceLang.trim() || "auto",
      targetLang: form.tradTargetLang.trim() || "ar",
    },
    llm: {
      baseUrl: form.baseUrl.trim(),
      apiKey: form.apiKey.trim(),
      model: form.model.trim(),
      temperature: Number(form.temperature) || 0.2,
      sourceLang: form.sourceLang.trim() || "English",
      targetLang: form.targetLang.trim() || "Arabic",
      systemPrompt: form.systemPrompt,
    },
    offlineLlm: {
      baseUrl: form.offBaseUrl.trim() || "http://127.0.0.1:8080/v1",
      model: form.offModel.trim() || "local-model",
      template: form.offTemplate || "auto",
      sourceLang: form.offSourceLang.trim() || "English",
      targetLang: form.offTargetLang.trim() || "Arabic",
      temperature: Number(form.offTemperature) || 0.1,
    },
    llamaCpp: {
      binaryPath: form.llamaBinaryPath.trim(),
      modelPath: form.llamaModelPath.trim(),
      port: Number(form.llamaPort) || 8080,
      gpuLayers: Number(form.llamaGpuLayers) || 0,
      ctxSize: Number(form.llamaCtxSize) || 4096,
      autoStart: Boolean(form.llamaAutoStart),
      extraArgs: form.llamaExtraArgs.trim(),
    },
  };
}

const isDirty = computed(() => {
  if (!config.value?.translator) return false;
  return (
    JSON.stringify(buildTranslatorConfig()) !==
    JSON.stringify(normalizeTranslator(config.value.translator))
  );
});

function onDiscard() {
  applyConfigToForm(config.value);
}

async function onSave() {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    translator: buildTranslatorConfig(),
  });
}

watch(
  () => form.active,
  (next) => {
    if (form.fallbackProvider === next) form.fallbackProvider = "";
  },
);

async function onSelectBenchmarkProvider(id) {
  form.active = id;
  if (form.fallbackProvider === id) form.fallbackProvider = "";
  await onSave();
}

async function onSelectFallbackProvider(id) {
  form.fallbackProvider = id === form.active ? "" : id;
  await onSave();
}

async function onTest() {
  testing.value = true;
  testErr.value = "";
  testResult.value = null;
  try {
    testResult.value = await testTranslateWithConfig(testInput.value, buildTranslatorConfig());
  } catch (e) {
    testErr.value = String(e);
  } finally {
    testing.value = false;
  }
}

async function onBrowseBinary() {
  const path = await browseLlamaBinary();
  if (path) form.llamaBinaryPath = path;
}

async function onBrowseModel() {
  const path = await browseGGUFModel();
  if (path) form.llamaModelPath = path;
}

async function onStartLlama() {
  await onSave();
  llamaBusy.value = true;
  llamaErr.value = "";
  try {
    const st = await startLlamaServer(buildTranslatorConfig().llamaCpp);
    if (st) llamaStatus.value = st;
    const base = `http://127.0.0.1:${Number(form.llamaPort) || 8080}/v1`;
    form.offBaseUrl = base;
    if (form.baseUrl.startsWith("http://127.0.0.1")) {
      form.baseUrl = base;
    }
    await onSave();
  } catch (e) {
    llamaErr.value = String(e);
    await refreshLlamaStatus();
  } finally {
    llamaBusy.value = false;
  }
}

async function onStopLlama() {
  llamaBusy.value = true;
  llamaErr.value = "";
  try {
    const st = await stopLlamaServer();
    if (st) llamaStatus.value = st;
  } finally {
    llamaBusy.value = false;
  }
}
</script>

<template>
  <div class="page">
    <section class="card">
      <div class="card-head">
        <div class="title-wrap">
          <Icon name="Globe" :size="15" class="title-ico" />
          <h2 class="h2">{{ t("trTitle") }}</h2>
        </div>

        <div class="summary-pill" :class="{ muted: form.active === 'none' }">
          <span class="dot" />
          <span class="sum-main">{{ activeLabel }}</span>
          <template v-if="fallbackLabel && form.active !== 'none'">
            <span class="sum-sep">·</span>
            <span class="sum-fb">{{ fallbackLabel }}</span>
          </template>
        </div>
      </div>

      <div class="pair-grid">
        <label class="pair-box">
          <span class="pair-title">{{ t("wizS3PrimaryLbl") }}</span>
          <select v-model="form.active" class="select">
            <optgroup :label="t('trCatTraditional')">
              <option v-for="p in TRADITIONAL_PROVIDERS" :key="p.id" :value="p.id">
                {{ p.label }}
              </option>
            </optgroup>
            <optgroup :label="t('trCatLLM')">
              <option value="offline_llm">Local Offline Translation Model (GGUF / llama.cpp)</option>
              <option value="llm">OpenAI-Compatible LLM (Cloud / Ollama / llama.cpp)</option>
            </optgroup>
            <option value="none">{{ t("trDisabled") }}</option>
          </select>
          <span class="pair-desc">{{ t("trPrimaryDesc") }}</span>
        </label>

        <label class="pair-box">
          <span class="pair-title">{{ t("trFallbackTitle") }}</span>
          <select v-model="form.fallbackProvider" class="select">
            <option value="">{{ t("trFallbackNone") }}</option>
            <option
              v-for="p in ALL_PROVIDERS.filter((x) => x.id !== form.active)"
              :key="p.id"
              :value="p.id"
            >
              {{ p.label }}
            </option>
          </select>
          <span class="pair-desc">{{ t("trFallbackDesc") }}</span>
        </label>
      </div>

      <div
        v-if="(isTraditional && (!isSimple || form.active === 'bing')) || form.active === 'offline_llm' || form.active === 'llm'"
        class="sub-panel"
      >
        <div v-if="isTraditional" class="grid">
          <template v-if="form.active === 'bing'">
            <label class="field">
              <span class="lbl">{{ t("trSourceLangCode") }}</span>
              <input v-model="form.bingSourceLang" class="input mono" placeholder="auto-detect, en, ja" />
            </label>
            <label class="field">
              <span class="lbl">{{ t("trTargetLangCode") }}</span>
              <input v-model="form.bingTargetLang" class="input mono" placeholder="ar" />
            </label>
          </template>
          <template v-else>
            <label class="field">
              <span class="lbl">{{ t("trSourceLangCode") }}</span>
              <input v-model="form.tradSourceLang" class="input mono" placeholder="auto, en, ja, zh-CN" />
            </label>
            <label class="field">
              <span class="lbl">{{ t("trTargetLangCode") }}</span>
              <input v-model="form.tradTargetLang" class="input mono" placeholder="ar" />
            </label>
          </template>
        </div>

        <div v-else-if="form.active === 'offline_llm'" class="grid">
          <label class="field">
            <span class="lbl">{{ t("trPromptTemplate") }}</span>
            <select v-model="form.offTemplate" class="select">
              <option v-for="tp in OFFLINE_TEMPLATES" :key="tp.id" :value="tp.id">{{ tp.label }}</option>
            </select>
          </label>

          <label class="field">
            <span class="lbl">{{ t("trLocalUrl") }}</span>
            <input v-model="form.offBaseUrl" class="input mono" placeholder="http://127.0.0.1:8080/v1" />
          </label>

          <label class="field">
            <span class="lbl">{{ t("trModelName") }}</span>
            <input v-model="form.offModel" class="input mono" placeholder="local-model" />
          </label>

          <div class="inline3">
            <label class="field">
              <span class="lbl">{{ t("trSourceLang") }}</span>
              <input v-model="form.offSourceLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">{{ t("trTargetLang") }}</span>
              <input v-model="form.offTargetLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">{{ t("trTemp") }}</span>
              <input v-model.number="form.offTemperature" type="number" step="0.05" min="0" max="2" class="input" />
            </label>
          </div>
        </div>

        <template v-else-if="form.active === 'llm'">
          <div class="chip-strip">
            <span class="lbl strip-lbl">{{ t("trQuickPresets") }}</span>
            <button
              v-for="p in PRESETS"
              :key="p.name"
              type="button"
              class="pill-chip"
              @click="applyPreset(p)"
            >
              {{ p.name }}
            </button>
          </div>

          <div class="grid">
            <label class="field">
              <span class="lbl">{{ t("trBaseUrl") }}</span>
              <input v-model="form.baseUrl" class="input mono" placeholder="https://api.openai.com/v1" />
            </label>

            <label class="field">
              <span class="lbl">{{ t("trModelName") }}</span>
              <input v-model="form.model" class="input mono" placeholder="gpt-4o-mini" />
            </label>

            <label class="field">
              <span class="lbl">{{ t("trApiKey") }}</span>
              <input v-model="form.apiKey" type="password" class="input mono" placeholder="sk-…" />
            </label>

            <div class="inline3">
              <label class="field">
                <span class="lbl">{{ t("trSourceLang") }}</span>
                <input v-model="form.sourceLang" class="input" />
              </label>
              <label class="field">
                <span class="lbl">{{ t("trTargetLang") }}</span>
                <input v-model="form.targetLang" class="input" />
              </label>
              <label class="field">
                <span class="lbl">{{ t("trTemp") }}</span>
                <input v-model.number="form.temperature" type="number" step="0.05" min="0" max="2" class="input" />
              </label>
            </div>
          </div>

          <label class="field">
            <span class="lbl">{{ t("trSystemPrompt") }}</span>
            <textarea v-model="form.systemPrompt" rows="3" class="textarea" />
          </label>
        </template>
      </div>

      <div class="test-box">
        <div class="test-strip">
          <input v-model="testInput" class="input flex1" dir="auto" />
          <button class="btn" type="button" :disabled="testing" @click="onTest">
            <Icon name="Play" :size="11" />
            <span>{{ testing ? t("trTesting") : t("trTestBtn") }}</span>
          </button>
        </div>
        <div v-if="testErr" class="err">{{ testErr }}</div>
        <div v-if="testResult" class="res">
          <div class="res-row">
            <strong>{{ t("trOutLogical") }}</strong>
            <span class="ar" dir="rtl">{{ testResult.translated }}</span>
            <span class="ms-pill" dir="ltr">{{ testResult.elapsedMs }} ms</span>
          </div>
          <div class="res-row">
            <strong>{{ t("trOutShaped") }}</strong>
            <span class="ar">{{ testResult.shaped }}</span>
          </div>
        </div>
      </div>

      <div class="save-bar" :class="{ dirty: isDirty }">
        <div v-if="isDirty" class="unsaved-msg">
          <span class="unsaved-dot" />
          <span>{{ t("embUnsavedHint") }}</span>
        </div>
        <div v-else class="saved-hint">
          {{ saveState === "saved" ? t("trSavedBtn") : "" }}
        </div>
        <div class="save-bar-btns">
          <button v-if="isDirty" class="btn" type="button" @click="onDiscard">
            {{ t("embDiscardChanges") }}
          </button>
          <button class="btn primary" :class="{ 'dirty-btn': isDirty }" type="button" @click="onSave">
            {{ saveState === "saving" ? t("embSaving") : saveState === "saved" ? t("trSavedBtn") : t("trSaveBtn") }}
          </button>
        </div>
      </div>
    </section>

    <details class="card collapsible-card">
      <summary class="section-summary">
        <span class="summary-left">
          <Icon name="Chevron" :size="13" class="chev" />
          <Icon name="Zap" :size="13" class="sum-ico" />
          <span>{{ t("benchTitle") }}</span>
        </span>
      </summary>
      <div class="collapsible-body">
        <TranslatorBenchmark
          :hide-title="true"
          :translator-config="buildTranslatorConfig()"
          :active-provider="form.active"
          :fallback-provider="form.fallbackProvider"
          @select-provider="onSelectBenchmarkProvider"
          @select-fallback="onSelectFallbackProvider"
        />
      </div>
    </details>

    <details class="card collapsible-card" :open="!isSimple || form.active === 'offline_llm'">
      <summary class="section-summary">
        <span class="summary-left">
          <Icon name="Chevron" :size="13" class="chev" />
          <span>{{ t("trAdvancedSection") }}</span>
        </span>
        <span class="status-pill" :class="{ ready: llamaStatus.ready, running: llamaStatus.running && !llamaStatus.ready }">
          {{ llamaStatus.ready ? `Ready · PID ${llamaStatus.pid}` : llamaStatus.running ? "Starting…" : "Stopped" }}
        </span>
      </summary>

      <div class="collapsible-body llama-body">
        <div class="head">
          <div class="head-left">
            <h2 class="h2">Local `llama.cpp` Server Runner</h2>
          </div>
          <div class="btn-row">
            <button class="btn" type="button" @click="showLlamaLogs = !showLlamaLogs">
              {{ showLlamaLogs ? "Hide Logs" : "Logs" }}
            </button>
            <button v-if="llamaStatus.running" class="btn danger" type="button" :disabled="llamaBusy" @click="onStopLlama">
              Stop Server
            </button>
            <button v-else class="btn primary" type="button" :disabled="llamaBusy" @click="onStartLlama">
              {{ llamaBusy ? "Starting…" : "Start Server" }}
            </button>
          </div>
        </div>

        <div class="grid">
          <label class="field">
            <span class="lbl">llama-server.exe Path</span>
            <div class="browse-row">
              <input v-model="form.llamaBinaryPath" class="input mono flex1" placeholder="C:\llama.cpp\llama-server.exe" />
              <button class="btn" type="button" @click="onBrowseBinary">Browse</button>
            </div>
          </label>

          <label class="field">
            <span class="lbl">GGUF Model File</span>
            <div class="browse-row">
              <input v-model="form.llamaModelPath" class="input mono flex1" placeholder="C:\Models\model.gguf" />
              <button class="btn" type="button" @click="onBrowseModel">Browse</button>
            </div>
          </label>

          <div class="inline3">
            <label class="field">
              <span class="lbl">Port</span>
              <input v-model.number="form.llamaPort" type="number" min="1" max="65535" class="input mono" />
            </label>
            <label class="field">
              <span class="lbl">GPU Layers (-ngl)</span>
              <input v-model.number="form.llamaGpuLayers" type="number" min="0" max="999" class="input mono" />
            </label>
            <label class="field">
              <span class="lbl">Ctx Size (-c)</span>
              <input v-model.number="form.llamaCtxSize" type="number" min="512" step="512" class="input mono" />
            </label>
          </div>

          <label class="field">
            <span class="lbl">Extra CLI Args (optional)</span>
            <input v-model="form.llamaExtraArgs" class="input mono" placeholder="--threads 8 --flash-attn" />
          </label>
        </div>

        <label class="chk">
          <input v-model="form.llamaAutoStart" type="checkbox" />
          <span>Auto-start `llama-server.exe` when Khutaf launches</span>
        </label>

        <div v-if="llamaErr || llamaStatus.lastError" class="err">{{ llamaErr || llamaStatus.lastError }}</div>
        <pre v-if="showLlamaLogs" class="llama-logs">{{ llamaStatus.recentLogs || "No server output yet." }}</pre>
      </div>
    </details>
  </div>
</template>

<style scoped>
.page {
  padding: 14px;
  overflow: auto;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.card {
  width: 100%;
  padding: 13px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}
.title-ico {
  color: var(--accent);
}
.h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.summary-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid color-mix(in srgb, var(--accent) 45%, var(--border));
  background: color-mix(in srgb, var(--accent) 10%, var(--paper));
  font-size: 11.5px;
  font-weight: 600;
}
.summary-pill .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #22c55e;
  box-shadow: 0 0 6px rgba(34, 197, 94, 0.55);
}
.summary-pill.muted {
  border-color: var(--border);
  background: var(--paper);
  color: var(--fg-muted);
}
.summary-pill.muted .dot {
  background: var(--fg-muted);
  box-shadow: none;
}
.sum-sep {
  opacity: 0.5;
}
.sum-fb {
  color: var(--fg-muted);
  font-weight: 500;
}

.pair-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 10px;
}
.pair-box {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 9px 11px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 7px;
}
.pair-title {
  font-size: 11.5px;
  font-weight: 600;
  color: var(--fg);
}
.pair-desc {
  font-size: 11px;
  line-height: 1.4;
  color: var(--fg-muted);
}

.chip-strip {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.strip-lbl {
  margin-inline-end: 2px;
}
.pill-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 4px 10px;
  background: var(--paper);
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 999px;
  font: inherit;
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: border-color 0.14s, background 0.14s, color 0.14s;
}
.pill-chip:hover {
  color: var(--fg);
  border-color: color-mix(in oklab, var(--fg) 25%, var(--border));
}
.pill-chip.active {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, var(--paper));
  color: var(--fg);
  font-weight: 600;
}
.chip-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--fg-muted);
  opacity: 0.4;
}
.pill-chip.active .chip-dot {
  background: var(--accent);
  opacity: 1;
}

.sub-panel {
  padding: 12px;
  background: color-mix(in oklab, var(--paper) 65%, var(--bg-elev));
  border: 1px solid var(--border);
  border-radius: 7px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.inline3 {
  display: grid;
  grid-template-columns: 1fr 1fr 80px;
  gap: 8px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.lbl {
  font-size: 11px;
  color: var(--fg-muted);
}
.input,
.select {
  width: 100%;
  height: 32px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font: inherit;
  font-size: 12px;
  min-width: 0;
}
.pair-box .select {
  background: var(--bg-elev);
}
.textarea {
  padding: 8px 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font: inherit;
  font-size: 12px;
  resize: vertical;
}
.mono {
  font-family: var(--mono);
}

.test-box {
  padding-top: 8px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.test-strip,
.browse-row,
.btn-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.flex1 {
  flex: 1;
  min-width: 0;
}
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.btn:hover:not(:disabled) {
  border-color: color-mix(in oklab, var(--fg) 25%, var(--border));
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
  font-weight: 600;
}
.btn.danger {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}

.save-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 2px 0 0;
  border-radius: 7px;
  background: transparent;
  border: 1px solid transparent;
}
.save-bar.dirty {
  padding: 8px 10px;
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 8%, var(--paper));
}
.unsaved-msg {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--fg);
}
.unsaved-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
}
.saved-hint {
  font-size: 12px;
  color: #4ade80;
  font-weight: 500;
}
.save-bar-btns {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-inline-start: auto;
}
.btn.dirty-btn {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 28%, transparent);
}

.collapsible-card {
  padding: 0;
  gap: 0;
}
.section-summary {
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 14px;
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
  cursor: pointer;
  user-select: none;
}
.section-summary::-webkit-details-marker {
  display: none;
}
.summary-left {
  display: inline-flex;
  align-items: center;
  gap: 7px;
}
.sum-ico {
  color: var(--accent);
}
.chev {
  color: var(--fg-muted);
  transition: transform 0.16s ease;
}
.collapsible-card[open] .chev {
  transform: rotate(90deg);
}
.collapsible-body {
  padding: 10px 14px 13px;
  border-top: 1px solid var(--border);
}
.llama-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.head-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.status-pill {
  padding: 2px 9px;
  border-radius: 999px;
  font-family: var(--mono);
  font-size: 11px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg-muted);
}
.status-pill.running {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
}
.status-pill.ready {
  background: color-mix(in oklab, #16a34a 22%, transparent);
  color: #4ade80;
}
.chk {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--fg);
  cursor: pointer;
}
.llama-logs {
  margin: 0;
  padding: 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-family: var(--mono);
  font-size: 11px;
  max-height: 180px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
.err {
  color: #f87171;
  font-size: 12px;
}
.res {
  padding: 7px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.res-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.ms-pill {
  margin-inline-start: auto;
  padding: 1px 7px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
  font-family: var(--mono);
  font-size: 10.5px;
  font-weight: 600;
}
.ar {
  font-family: var(--arabic);
  font-size: 14px;
}
</style>
