<script setup>
import { reactive, shallowRef, watch } from "vue";
import { useSession } from "../composables/useSession.js";

const { config, saveConfig, saveState, testTranslate } = useSession();

const PRESETS = [
  { name: "OpenAI", url: "https://api.openai.com/v1", model: "gpt-4o-mini" },
  { name: "Gemini (OpenAI compat)", url: "https://generativelanguage.googleapis.com/v1beta/openai", model: "gemini-2.5-flash" },
  { name: "DeepSeek", url: "https://api.deepseek.com/v1", model: "deepseek-chat" },
  { name: "Groq", url: "https://api.groq.com/openai/v1", model: "llama-3.3-70b-versatile" },
  { name: "OpenRouter", url: "https://openrouter.ai/api/v1", model: "google/gemini-2.5-flash" },
  { name: "Local Ollama", url: "http://127.0.0.1:11434/v1", model: "qwen2.5:7b" },
  { name: "Local llama.cpp / LM Studio", url: "http://127.0.0.1:8080/v1", model: "local-model" },
];

const form = reactive({
  active: "bing",
  bingSourceLang: "auto-detect",
  bingTargetLang: "ar",
  baseUrl: "",
  apiKey: "",
  model: "",
  temperature: 0.2,
  sourceLang: "English",
  targetLang: "Arabic",
  systemPrompt: "",
});

const testInput = shallowRef("The ancient gate is sealed. Find the Silver Key to proceed!");
const testResult = shallowRef(null);
const testErr = shallowRef("");
const testing = shallowRef(false);

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    form.active = cfg.translator.active || "bing";
    form.bingSourceLang = cfg.translator.bing?.sourceLang || "auto-detect";
    form.bingTargetLang = cfg.translator.bing?.targetLang || "ar";
    Object.assign(form, cfg.translator.llm);
  },
  { immediate: true },
);

function applyPreset(p) {
  form.active = "llm";
  form.baseUrl = p.url;
  form.model = p.model;
}

async function onSave() {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    translator: {
      active: form.active,
      bing: {
        sourceLang: form.bingSourceLang.trim() || "auto-detect",
        targetLang: form.bingTargetLang.trim() || "ar",
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
    },
  });
}

async function onTest() {
  await onSave();
  testing.value = true;
  testErr.value = "";
  testResult.value = null;
  try {
    testResult.value = await testTranslate(testInput.value);
  } catch (e) {
    testErr.value = String(e);
  } finally {
    testing.value = false;
  }
}
</script>

<template>
  <div class="page">
    <div class="card">
      <div class="head">
        <h2 class="h2">Translation Provider</h2>
        <select v-model="form.active" class="select">
          <option value="bing">Bing Traditional (Free, No API Key)</option>
          <option value="llm">OpenAI-Compatible LLM (Cloud / Ollama / llama.cpp)</option>
          <option value="none">Disabled (Pass-through)</option>
        </select>
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
            {{ testing ? "Testing…" : "Save & Test Translate" }}
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
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  overflow: auto;
  height: 100%;
}
.card {
  max-width: 820px;
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
.chip:hover {
  border-color: var(--accent);
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.inline3 {
  display: grid;
  grid-template-columns: 1fr 1fr 70px;
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
.test-row {
  display: flex;
  gap: 8px;
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
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
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
