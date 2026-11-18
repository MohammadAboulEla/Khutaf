<script setup>
import { computed, onMounted, onUnmounted, reactive, shallowRef, watch } from "vue";
import { useSession } from "../composables/useSession.js";

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

const TRADITIONAL_PROVIDERS = [
  { id: "bing", label: "Bing" },
  { id: "google", label: "Google" },
  { id: "microsoft", label: "Microsoft" },
  { id: "yandex", label: "Yandex" },
  { id: "modernmt", label: "ModernMT" },
  { id: "alibaba", label: "Alibaba" },
  { id: "youdao", label: "Youdao" },
  { id: "papago", label: "Papago" },
  { id: "qqtransmart", label: "qqTranSmart" },
  { id: "qqimt", label: "qqimt" },
  { id: "translatecom", label: "Translate.com" },
  { id: "volcengine", label: "Volcengine" },
  { id: "lingocloud", label: "LingoCloud" },
];

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

const isTraditional = computed(() =>
  TRADITIONAL_PROVIDERS.some((p) => p.id === form.active),
);

const testInput = shallowRef("The ancient gate is sealed. Find the Silver Key to proceed!");
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

onMounted(() => {
  refreshLlamaStatus();
  llamaTimer = setInterval(refreshLlamaStatus, 3000);
});

onUnmounted(() => {
  clearInterval(llamaTimer);
});

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    form.active = cfg.translator.active || "bing";
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
  },
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

async function onSave() {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    translator: buildTranslatorConfig(),
  });
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
    <div class="card">
      <div class="head">
        <h2 class="h2">Translation Provider</h2>
        <select v-model="form.active" class="select">
          <optgroup label="Free Traditional Web Translators (No API Key)">
            <option v-for="p in TRADITIONAL_PROVIDERS" :key="p.id" :value="p.id">
              {{ p.label }} (Free Web)
            </option>
          </optgroup>
          <optgroup label="Local &amp; Cloud LLMs">
            <option value="offline_llm">Local Offline Translation Model (GGUF / llama.cpp)</option>
            <option value="llm">OpenAI-Compatible LLM (Cloud / Ollama / llama.cpp)</option>
          </optgroup>
          <option value="none">Disabled (Pass-through)</option>
        </select>
      </div>

      <template v-if="isTraditional">
        <div class="presets">
          <span class="lbl">Traditional Translators:</span>
          <button
            v-for="p in TRADITIONAL_PROVIDERS"
            :key="p.id"
            type="button"
            class="chip"
            :class="{ active: form.active === p.id }"
            @click="form.active = p.id"
          >
            {{ p.label }}
          </button>
        </div>

        <div v-if="form.active === 'bing'" class="grid">
          <label class="field">
            <span class="lbl">Source Language Code</span>
            <input v-model="form.bingSourceLang" class="input mono" placeholder="auto-detect, en, ja" />
          </label>
          <label class="field">
            <span class="lbl">Target Language Code</span>
            <input v-model="form.bingTargetLang" class="input mono" placeholder="ar" />
          </label>
        </div>
        <div v-else class="grid">
          <label class="field">
            <span class="lbl">Source Language Code</span>
            <input v-model="form.tradSourceLang" class="input mono" placeholder="auto, en, ja, zh-CN" />
          </label>
          <label class="field">
            <span class="lbl">Target Language Code</span>
            <input v-model="form.tradTargetLang" class="input mono" placeholder="ar" />
          </label>
        </div>
      </template>

      <template v-else-if="form.active === 'offline_llm'">
        <div class="grid">
          <label class="field">
            <span class="lbl">Prompt Template</span>
            <select v-model="form.offTemplate" class="select">
              <option v-for="t in OFFLINE_TEMPLATES" :key="t.id" :value="t.id">{{ t.label }}</option>
            </select>
          </label>

          <label class="field">
            <span class="lbl">Local Server Base URL</span>
            <input v-model="form.offBaseUrl" class="input mono" placeholder="http://127.0.0.1:8080/v1" />
          </label>

          <label class="field">
            <span class="lbl">Model Name</span>
            <input v-model="form.offModel" class="input mono" placeholder="local-model" />
          </label>

          <div class="inline3">
            <label class="field">
              <span class="lbl">Source Lang</span>
              <input v-model="form.offSourceLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">Target Lang</span>
              <input v-model="form.offTargetLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">Temp</span>
              <input v-model.number="form.offTemperature" type="number" step="0.05" min="0" max="2" class="input" />
            </label>
          </div>
        </div>
      </template>

      <template v-else-if="form.active === 'llm'">
        <div class="presets">
          <span class="lbl">Quick Endpoint Presets:</span>
          <button
            v-for="p in PRESETS"
            :key="p.name"
            type="button"
            class="chip"
            @click="applyPreset(p)"
          >
            {{ p.name }}
          </button>
        </div>

        <div class="grid">
          <label class="field">
            <span class="lbl">Base URL (`/chat/completions` appended automatically)</span>
            <input v-model="form.baseUrl" class="input mono" placeholder="https://api.openai.com/v1" />
          </label>

          <label class="field">
            <span class="lbl">Model Name</span>
            <input v-model="form.model" class="input mono" placeholder="gpt-4o-mini" />
          </label>

          <label class="field">
            <span class="lbl">API Key (leave empty for local Ollama / llama.cpp)</span>
            <input v-model="form.apiKey" type="password" class="input mono" placeholder="sk-…" />
          </label>

          <div class="inline3">
            <label class="field">
              <span class="lbl">Source Lang</span>
              <input v-model="form.sourceLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">Target Lang</span>
              <input v-model="form.targetLang" class="input" />
            </label>
            <label class="field">
              <span class="lbl">Temp</span>
              <input v-model.number="form.temperature" type="number" step="0.05" min="0" max="2" class="input" />
            </label>
          </div>
        </div>

        <label class="field">
          <span class="lbl">System Prompt (`{source}` and `{target}` are substituted)</span>
          <textarea v-model="form.systemPrompt" rows="3" class="textarea" />
        </label>
      </template>

      <div class="test-box">
        <div class="test-row">
          <input v-model="testInput" class="input flex1" />
          <button class="btn" type="button" :disabled="testing" @click="onTest">
            {{ testing ? "Testing…" : "Test Translate" }}
          </button>
          <button class="btn primary" type="button" @click="onSave">
            {{ saveState === "saved" ? "Saved ✓" : "Save" }}
          </button>
        </div>
        <div v-if="testErr" class="err">{{ testErr }}</div>
        <div v-if="testResult" class="res">
          <div><strong>Arabic (Logical):</strong> <span class="ar" dir="rtl">{{ testResult.translated }}</span></div>
          <div><strong>Shaped (Visual):</strong> <span class="ar">{{ testResult.shaped }}</span> ({{ testResult.elapsedMs }} ms)</div>
        </div>
      </div>
    </div>

    <div class="card">
      <div class="head">
        <div class="head-left">
          <h2 class="h2">Local `llama.cpp` Server Runner</h2>
          <span class="status-pill" :class="{ ready: llamaStatus.ready, running: llamaStatus.running && !llamaStatus.ready }">
            {{ llamaStatus.ready ? `Ready · PID ${llamaStatus.pid}` : llamaStatus.running ? "Starting…" : "Stopped" }}
          </span>
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
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  overflow: auto;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.card {
  max-width: 860px;
  padding: 16px 18px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 12px;
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
.h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.presets {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.chip {
  padding: 3px 8px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.chip:hover,
.chip.active {
  border-color: var(--accent);
}
.chip.active {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
  font-weight: 600;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
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
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  min-width: 0;
}
.textarea {
  padding: 8px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
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
  gap: 8px;
}
.test-row,
.browse-row,
.btn-row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.flex1 {
  flex: 1;
}
.btn {
  height: 30px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.btn.danger {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}
.status-pill {
  padding: 2px 8px;
  border-radius: 4px;
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
  border-radius: 5px;
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
  padding: 8px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ar {
  font-family: var(--arabic);
  font-size: 14px;
}
</style>
