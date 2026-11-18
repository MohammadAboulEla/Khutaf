<script setup>
import { reactive, ref, shallowRef, watch, onMounted } from "vue";
import { useSession } from "../composables/useSession.js";

const { config, saveConfig, saveState, listFonts, unityFontStatus, refreshUnityFontStatus, downloadUnityFonts } =
  useSession();

const fonts = shallowRef(["Tahoma", "Arial", "Segoe UI"]);
const confirmRedownload = ref(false);

onMounted(async () => {
  refreshUnityFontStatus();
  const list = await listFonts();
  if (list?.length) fonts.value = list;
});

function onDownloadClick() {
  if (unityFontStatus.value.installed && !confirmRedownload.value) {
    confirmRedownload.value = true;
    return;
  }
  confirmRedownload.value = false;
  downloadUnityFonts();
}

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
});

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    Object.assign(form, cfg.embed);
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
  });
}
</script>

<template>
  <div class="page">
    <div class="card compact-card">
      <div class="bundle-row">
        <div class="bundle-header">
          <div class="bundle-title-row">
            <h2 class="h2">Unity TMP Font AssetBundles</h2>
            <span class="badge" :class="unityFontStatus.installed ? 'ok' : 'missing'">
              {{ unityFontStatus.installed ? `Installed (${unityFontStatus.count})` : "Not Installed" }}
            </span>
          </div>
          <p class="bundle-desc">
            Optional ~139 MB fallback font pack for Unity games when OS dynamic font creation fails.
          </p>
        </div>

        <div class="bundle-actions">
          <template v-if="unityFontStatus.installed && confirmRedownload && !unityFontStatus.downloading">
            <span class="confirm-hint">Re-download ~139 MB?</span>
            <button class="btn secondary" type="button" @click="confirmRedownload = false">Cancel</button>
            <button class="btn secondary warn" type="button" @click="onDownloadClick">Confirm</button>
          </template>
          <button
            v-else
            class="btn secondary"
            type="button"
            :disabled="unityFontStatus.downloading"
            @click="onDownloadClick"
          >
            {{
              unityFontStatus.downloading
                ? "Downloading…"
                : unityFontStatus.installed
                  ? "Re-download"
                  : "Download Bundles (~139 MB)"
            }}
          </button>
        </div>
      </div>

      <div v-if="unityFontStatus.downloading || unityFontStatus.error" class="bundle-progress">
        <div v-if="unityFontStatus.downloading" class="progress-track">
          <div class="progress-fill" :style="{ width: `${unityFontStatus.progress}%` }"></div>
        </div>
        <span class="bundle-msg" :class="{ err: unityFontStatus.error }">
          {{ unityFontStatus.error || unityFontStatus.message }}
        </span>
      </div>
    </div>

    <div class="card">
      <h2 class="h2">In-Game Embedded Arabic Settings</h2>

      <div class="grid-3">
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
      </div>

      <div class="grid-2">
        <label class="field" title="Takes effect on next game launch">
          <span class="lbl">Override Game Font Family <span class="hint">(next game launch)</span></span>
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
          <span>Strip Typewriter Alpha Tags — may improve performance</span>
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
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.card {
  width: 100%;
  padding: 16px 18px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.compact-card {
  padding: 12px 18px;
  gap: 10px;
}
.bundle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.grid-3 {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  align-items: end;
}
.grid-2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-items: end;
}
.hint {
  opacity: 0.7;
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
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px 24px;
  padding-top: 6px;
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
.bundle-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.bundle-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.bundle-desc {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.bundle-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.confirm-hint {
  font-size: 12px;
  color: var(--fg-muted);
}
.badge {
  font-size: 11px;
  padding: 2px 7px;
  border-radius: 4px;
  border: 1px solid var(--border);
}
.badge.ok {
  color: #3fb950;
  border-color: rgba(63, 185, 80, 0.35);
}
.badge.missing {
  color: var(--fg-muted);
}
.btn.secondary {
  height: 28px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn.secondary.warn {
  border-color: rgba(248, 81, 73, 0.45);
  color: #f85149;
}
.btn.secondary:disabled {
  opacity: 0.6;
  cursor: default;
}
.bundle-progress {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.progress-track {
  height: 6px;
  background: var(--bg-elev);
  border-radius: 3px;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: var(--accent);
  transition: width 150ms ease;
}
.bundle-msg {
  font-size: 11px;
  color: var(--fg-muted);
}
.bundle-msg.err {
  color: #f85149;
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
