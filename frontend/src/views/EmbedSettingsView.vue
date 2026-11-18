<script setup>
import { reactive, shallowRef, watch, onMounted } from "vue";
import { useSession } from "../composables/useSession.js";

const { config, saveConfig, saveState, listFonts } = useSession();

const fonts = shallowRef(["Tahoma", "Arial", "Segoe UI"]);

onMounted(async () => {
  const list = await listFonts();
  if (list?.length) fonts.value = list;
});

const form = reactive({
  timeoutMs: 2000,
  displayMode: 0,
  clearText: false,
  changeFont: true,
  fontFamily: "Tahoma",
  useUnityBundle: false,
  changeFontSize: false,
  fontSizeRelative: 1.0,
  limitLineLength: false,
  maxLineLength: 40,
  stripTypewriterTags: true,
  flushDelayMs: 500,
  codepage: 932,
});

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    Object.assign(form, cfg.embed, {
      flushDelayMs: cfg.hook.flushDelayMs,
      codepage: cfg.hook.codepage,
    });
  },
  { immediate: true },
);

function onSave() {
  if (!config.value) return;
  saveConfig({
    ...config.value,
    embed: {
      timeoutMs: Number(form.timeoutMs) || 2000,
      displayMode: Number(form.displayMode),
      clearText: form.clearText,
      changeFont: form.changeFont,
      fontFamily: form.fontFamily.trim(),
      useUnityBundle: form.useUnityBundle,
      changeFontSize: form.changeFontSize,
      fontSizeRelative: Number(form.fontSizeRelative) || 1.0,
      limitLineLength: form.limitLineLength,
      maxLineLength: Number(form.maxLineLength) || 40,
      stripTypewriterTags: form.stripTypewriterTags,
    },
    hook: {
      ...config.value.hook,
      flushDelayMs: Number(form.flushDelayMs) || 500,
      codepage: Number(form.codepage) || 932,
    },
  });
}
</script>

<template>
  <div class="page">
    <div class="card">
      <h2 class="h2">In-Game Embedded Arabic Settings</h2>

      <div class="grid">
        <label class="field">
          <span class="lbl">Display Mode</span>
          <select v-model.number="form.displayMode" class="input">
            <option :value="0">Translation Only (Arabic)</option>
            <option :value="1">Original + Translation (Newline Below)</option>
            <option :value="2">Translation + Original</option>
            <option :value="3">Original Only (Bypass)</option>
          </select>
        </label>

        <label class="field">
          <span class="lbl">Translation Timeout (ms)</span>
          <input v-model.number="form.timeoutMs" type="number" min="200" max="15000" step="100" class="input" />
        </label>

        <label class="field">
          <span class="lbl">Logical Line Wrap Length (chars)</span>
          <input v-model.number="form.maxLineLength" type="number" min="15" max="120" class="input" />
        </label>

        <label class="field">
          <span class="lbl">Override Game Font Family (takes effect on next game launch)</span>
          <select v-model="form.fontFamily" class="input">
            <option v-if="form.fontFamily && !fonts.includes(form.fontFamily)" :value="form.fontFamily">
              {{ form.fontFamily }}
            </option>
            <option v-for="f in fonts" :key="f" :value="f">{{ f }}</option>
          </select>
        </label>

        <label class="field">
          <span class="lbl">Relative Font Scale</span>
          <input v-model.number="form.fontSizeRelative" type="number" min="0.5" max="2.5" step="0.05" class="input" />
        </label>

        <label class="field">
          <span class="lbl">Hook Flush Delay (ms) / Legacy Codepage</span>
          <div class="inline">
            <input v-model.number="form.flushDelayMs" type="number" min="50" max="2000" class="input" />
            <input v-model.number="form.codepage" type="number" class="input" />
          </div>
        </label>
      </div>

      <div class="checks">
        <label class="chk">
          <input v-model="form.changeFont" type="checkbox" />
          <span>Override Game Font (OS Font / Unity Dynamic TMP)</span>
        </label>
        <label class="chk">
          <input v-model="form.useUnityBundle" type="checkbox" />
          <span>Use Unity Arial SDF Bundle (fallback if OS font fails)</span>
        </label>
        <label class="chk">
          <input v-model="form.changeFontSize" type="checkbox" />
          <span>Apply Relative Font Scale</span>
        </label>
        <label class="chk">
          <input v-model="form.clearText" type="checkbox" />
          <span>Clear Original In-Game Text Buffer</span>
        </label>
        <label class="chk">
          <input v-model="form.limitLineLength" type="checkbox" />
          <span>Force Line Wrap on Non-Arabic Output Too</span>
        </label>
        <label class="chk" title="Strips invisible &lt;color=#00000000&gt; typewriter tags while preserving visible text colors. Improves performance and prevents lag in animated dialogue.">
          <input v-model="form.stripTypewriterTags" type="checkbox" />
          <span>Strip Typewriter Alpha Tags (&lt;color=#00000000&gt;) — may improve performance</span>
        </label>
      </div>

      <div class="actions">
        <button class="btn primary" type="button" @click="onSave">
          {{ saveState === "saving" ? "Saving…" : saveState === "saved" ? "Saved ✓" : "Apply & Save" }}
        </button>
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
  max-width: 780px;
  padding: 16px 18px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
}
.lbl {
  font-size: 11px;
  color: var(--fg-muted);
}
.input {
  width: 100%;
  min-width: 0;
  height: 32px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.inline {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.checks {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  padding-top: 4px;
}
.chk {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  cursor: pointer;
}
.actions {
  display: flex;
  justify-content: flex-end;
}
.btn.primary {
  height: 32px;
  padding: 0 16px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
</style>
