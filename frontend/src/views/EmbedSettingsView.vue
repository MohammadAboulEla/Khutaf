<script setup>
import { computed, reactive, ref, shallowRef, watch, onMounted, onUnmounted } from "vue";
import Icon from "../components/Icon.vue";
import Tooltip from "../components/Tooltip.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { useUiMode } from "../composables/useUiMode.js";

const props = defineProps({
  onboardingOpen: { type: Boolean, default: false },
});

const { config, saveConfig, saveState, listFonts, listAllFonts, unityFontStatus, refreshUnityFontStatus, openFontDownloadPrompt } =
  useSession();
const { t } = useI18n();
const { isSimple } = useUiMode();

const fonts = shallowRef(["Tahoma", "Arial", "Segoe UI"]);
const showAllFonts = ref(false);
const confirmRedownload = ref(false);
const showBundleModal = ref(false);
const scalePresets = [0.85, 1.0, 1.15, 1.3];

async function refreshFonts() {
  const list = showAllFonts.value ? await listAllFonts() : await listFonts();
  if (list?.length) fonts.value = list;
}

watch(showAllFonts, () => {
  refreshFonts();
});

function onKeyDown(e) {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    e.preventDefault();
    if (isDirty.value) onSave();
  }
}

onMounted(async () => {
  window.addEventListener("keydown", onKeyDown);
  const st = await refreshUnityFontStatus();
  if (
    !props.onboardingOpen &&
    config.value?.ui?.onboarded !== false &&
    st &&
    !st.installed &&
    !st.downloading &&
    !localStorage.getItem("khutaf.unityBundlePrompted")
  ) {
    localStorage.setItem("khutaf.unityBundlePrompted", "1");
    showBundleModal.value = true;
  }
  await refreshFonts();
});

onUnmounted(() => {
  window.removeEventListener("keydown", onKeyDown);
});

function onDownloadFromModal() {
  showBundleModal.value = false;
  openFontDownloadPrompt();
}

function onDownloadClick() {
  if (unityFontStatus.value.installed && !confirmRedownload.value) {
    confirmRedownload.value = true;
    return;
  }
  confirmRedownload.value = false;
  openFontDownloadPrompt();
}

const form = reactive({
  timeoutMs: 2000,
  displayMode: 0,
  clearText: false,
  changeFont: true,
  fontFamily: "Tahoma",
  useUnityBundle: true,
  changeFontSize: false,
  fontSizeRelative: 1.0,
  limitLineLength: false,
  maxLineLength: 40,
  stripTypewriterTags: true,
  nonBlocking: true,
  pendingText: "",
  verboseHooks: false,
});

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    Object.assign(form, cfg.embed);
  },
  { immediate: true },
);

const isDirty = computed(() => {
  const saved = config.value?.embed;
  if (!saved) return false;
  return (
    (Number(form.timeoutMs) || 2000) !== Number(saved.timeoutMs) ||
    Number(form.displayMode) !== Number(saved.displayMode) ||
    Boolean(form.clearText) !== Boolean(saved.clearText) ||
    Boolean(form.changeFont) !== Boolean(saved.changeFont) ||
    form.fontFamily.trim() !== (saved.fontFamily || "").trim() ||
    Boolean(form.useUnityBundle) !== Boolean(saved.useUnityBundle) ||
    Boolean(form.changeFontSize) !== Boolean(saved.changeFontSize) ||
    Math.abs((Number(form.fontSizeRelative) || 1.0) - (Number(saved.fontSizeRelative) || 1.0)) > 0.001 ||
    Boolean(form.limitLineLength) !== Boolean(saved.limitLineLength) ||
    (Number(form.maxLineLength) || 40) !== Number(saved.maxLineLength) ||
    Boolean(form.stripTypewriterTags) !== Boolean(saved.stripTypewriterTags) ||
    Boolean(form.nonBlocking) !== Boolean(saved.nonBlocking) ||
    Boolean(form.verboseHooks) !== Boolean(saved.verboseHooks) ||
    form.pendingText.trim() !== (saved.pendingText || "").trim()
  );
});

function onDiscard() {
  if (!config.value?.embed) return;
  Object.assign(form, config.value.embed);
}

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
      nonBlocking: form.nonBlocking,
      pendingText: form.pendingText.trim(),
      verboseHooks: Boolean(form.verboseHooks),
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
            <h2 class="h2">{{ t("embBundlesTitle") }}</h2>
            <span class="badge" :class="unityFontStatus.installed ? 'ok' : 'missing'">
              {{ unityFontStatus.installed ? t("embInstalled", { count: unityFontStatus.count }) : t("embNotInstalled") }}
            </span>
          </div>
          <p class="bundle-desc">{{ t("embBundlesDesc") }}</p>
        </div>

        <div class="bundle-actions">
          <template v-if="unityFontStatus.installed && confirmRedownload && !unityFontStatus.downloading">
            <span class="confirm-hint">~139 MB?</span>
            <button class="btn secondary" type="button" @click="confirmRedownload = false">{{ t("benchCancel") }}</button>
            <button class="btn secondary warn" type="button" @click="onDownloadClick">{{ t("embRedownload") }}</button>
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
                ? t("embDownloading")
                : unityFontStatus.installed
                  ? t("embRedownload")
                  : t("embDownloadBtn")
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
      <h2 class="h2">{{ t("embTitle") }}</h2>

      <div class="grid-3">
        <label class="field">
          <span class="lbl">{{ t("embDisplayMode") }}</span>
          <select v-model.number="form.displayMode" class="input">
            <option :value="0">{{ t("embOptTransOnly") }}</option>
            <option :value="1">{{ t("embOptOriTrans") }}</option>
            <option :value="2">{{ t("embOptTransOri") }}</option>
            <option :value="3">{{ t("embOptOriOnly") }}</option>
          </select>
        </label>

        <label v-if="!isSimple" class="field">
          <span class="lbl">{{ t("embTimeout") }}</span>
          <input v-model.number="form.timeoutMs" type="number" min="200" max="15000" step="100" class="input" />
        </label>
      </div>

      <div class="font-grid">
        <div class="sub-panel">
          <label class="chk strong-chk">
            <input v-model="form.changeFont" type="checkbox" />
            <span>{{ t("embChangeFont") }}</span>
          </label>

          <label class="field" :class="{ disabled: !form.changeFont }">
            <span class="lbl">{{ t("embFontFamily") }}</span>
            <select v-model="form.fontFamily" class="input" :disabled="!form.changeFont">
              <option v-if="form.fontFamily && !fonts.includes(form.fontFamily)" :value="form.fontFamily">
                {{ form.fontFamily }}
              </option>
              <option v-for="f in fonts" :key="f" :value="f">{{ f }}</option>
            </select>
            <span class="field-hint">{{ t("embFontFamilyHint") }}</span>
          </label>

          <label class="chk" :class="{ disabled: !form.changeFont }">
            <input v-model="showAllFonts" type="checkbox" :disabled="!form.changeFont" />
            <span>{{ t("embShowAllFonts") }}</span>
          </label>

          <Tooltip :text="unityFontStatus.installed ? '' : 'Download bundles first to enable this fallback'">
            <label class="chk" :class="{ disabled: !unityFontStatus.installed || !form.changeFont }">
              <input
                v-model="form.useUnityBundle"
                type="checkbox"
                :disabled="!unityFontStatus.installed || !form.changeFont"
              />
              <span>{{ t("embUseBundleChk") }}</span>
            </label>
          </Tooltip>
        </div>

        <div class="sub-panel">
          <label class="chk strong-chk">
            <input v-model="form.changeFontSize" type="checkbox" />
            <span>{{ t("embChangeScale") }}</span>
          </label>

          <label class="field" :class="{ disabled: !form.changeFontSize }">
            <span class="lbl">{{ t("embFontScale") }}</span>
            <input
              v-model.number="form.fontSizeRelative"
              type="number"
              min="0.5"
              max="2.5"
              step="0.05"
              class="input"
              :disabled="!form.changeFontSize"
            />
          </label>

          <div class="scale-presets">
            <button
              v-for="p in scalePresets"
              :key="p"
              type="button"
              class="preset-btn"
              :class="{ active: form.changeFontSize && Math.abs(Number(form.fontSizeRelative) - p) < 0.01 }"
              :disabled="!form.changeFontSize"
              @click="form.fontSizeRelative = p"
            >
              {{ p }}×
            </button>
          </div>

          <div class="preview-wrap">
            <span class="lbl">{{ t("embFontPreviewLbl") }}</span>
            <div
              class="font-preview"
              dir="rtl"
              :style="{
                fontFamily: form.fontFamily ? `'${form.fontFamily}', sans-serif` : 'inherit',
                fontSize: `${Math.round(14 * (form.changeFontSize ? Number(form.fontSizeRelative) || 1 : 1))}px`,
              }"
            >
              {{ t("embFontPreview") }}
            </div>
          </div>
        </div>
      </div>

      <details class="adv-details" :open="!isSimple">
        <summary class="adv-summary">{{ t("embAdvancedGroup") }}</summary>
        <div class="adv-body">
          <div class="adv-grid">
            <label class="field">
              <span class="lbl">{{ t("embLineLen") }}</span>
              <input v-model.number="form.maxLineLength" type="number" min="15" max="120" class="input" />
            </label>
            <label v-if="isSimple" class="field">
              <span class="lbl">{{ t("embTimeout") }}</span>
              <input v-model.number="form.timeoutMs" type="number" min="200" max="15000" step="100" class="input" />
            </label>
          </div>
          <div class="checks">
            <label class="chk">
              <input v-model="form.clearText" type="checkbox" />
              <span>{{ t("embClearBuffer") }}</span>
            </label>
            <label class="chk">
              <input v-model="form.limitLineLength" type="checkbox" />
              <span>{{ t("embWrapNonArabic") }}</span>
            </label>
            <label class="chk">
              <input v-model="form.stripTypewriterTags" type="checkbox" />
              <span>{{ t("embStripTypewriter") }}</span>
            </label>
            <label class="chk">
              <input v-model="form.nonBlocking" type="checkbox" />
              <span>{{ t("embNonBlocking") }}</span>
            </label>
            <Tooltip :text="t('verboseHooksTip')">
              <label class="chk">
                <input v-model="form.verboseHooks" type="checkbox" />
                <span>{{ t("verboseHooksLabel") }}</span>
              </label>
            </Tooltip>
          </div>

          <div class="field pending-box" :class="{ disabled: !form.nonBlocking }">
            <span class="lbl pending-title">{{ t("embPendingTitle") }}</span>
            <p class="pending-desc">{{ t("embPendingDesc") }}</p>
            <input
              v-model="form.pendingText"
              type="text"
              maxlength="64"
              dir="auto"
              :placeholder="t('embPendingPlaceholder')"
              :disabled="!form.nonBlocking"
              class="input"
            />
            <span v-if="!form.nonBlocking" class="pending-disabled-hint">
              {{ t("embPendingDisabledHint") }}
            </span>
          </div>
        </div>
      </details>

      <div class="save-bar" :class="{ dirty: isDirty }">
        <div v-if="isDirty" class="unsaved-msg">
          <span class="unsaved-dot"></span>
          <span>{{ t("embUnsavedHint") }}</span>
        </div>
        <div class="save-bar-btns">
          <button v-if="isDirty" class="btn secondary" type="button" @click="onDiscard">
            {{ t("embDiscardChanges") }}
          </button>
          <button class="btn primary" :class="{ 'dirty-btn': isDirty }" type="button" @click="onSave">
            {{ saveState === "saving" ? t("embSaving") : saveState === "saved" ? t("trSavedBtn") : t("embApplySave") }}
          </button>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <Transition name="fade">
        <div v-if="showBundleModal" class="modal-overlay" @click.self="showBundleModal = false">
          <div class="modal-box" role="dialog" aria-modal="true" aria-label="Unity TMP Font AssetBundles">
            <header class="modal-head">
              <span class="modal-title">Unity TMP Font AssetBundles Recommended</span>
              <button class="modal-close" type="button" aria-label="Close" @click="showBundleModal = false">
                <Icon name="X" :size="14" />
              </button>
            </header>
            <div class="modal-body">
              <p>
                <strong>Use Unity Arial SDF Bundle</strong> is enabled by default. Downloading the
                optional <strong>~139 MB</strong> font bundle pack is recommended so Arabic text renders cleanly when
                dynamic OS font creation fails.
              </p>
              <p class="modal-sub">You can also download or re-download it anytime from the top of this tab.</p>
            </div>
            <footer class="modal-foot">
              <button class="btn secondary" type="button" @click="showBundleModal = false">Later</button>
              <button class="btn primary" type="button" @click="onDownloadFromModal">Download Now (~139 MB)</button>
            </footer>
          </div>
        </div>
      </Transition>
    </Teleport>
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
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
  align-items: end;
}
.adv-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 12px;
  align-items: end;
}
.font-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
  align-items: stretch;
}
.sub-panel {
  padding: 12px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.sub-panel .input {
  background: var(--bg-elev);
}
.sub-panel .field {
  flex: 0 0 auto;
}
.strong-chk {
  font-weight: 600;
}
.scale-presets {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.preset-btn {
  height: 24px;
  padding: 0 8px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--bg-elev);
  color: var(--fg-muted);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.preset-btn.active {
  color: var(--fg);
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 12%, var(--bg-elev));
}
.preset-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.preview-wrap {
  margin-top: auto;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.font-preview {
  min-height: 44px;
  display: flex;
  align-items: center;
  padding: 8px 10px;
  border-radius: 5px;
  background: var(--bg-elev);
  border: 1px dashed var(--border);
  font-size: 14px;
  line-height: 1.5;
  color: var(--fg);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.hint {
  opacity: 0.7;
}
.field-hint {
  font-size: 11px;
  color: var(--fg-muted);
  line-height: 1.4;
}
.field-tip {
  width: 100%;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 5px;
  min-width: 0;
  flex: 1;
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
.field.disabled {
  opacity: 0.55;
}
.pending-box {
  padding: 10px 12px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  gap: 6px;
}
.pending-title {
  color: var(--fg);
  font-weight: 600;
}
.pending-desc {
  margin: 0;
  font-size: 11.5px;
  color: var(--fg-muted);
  line-height: 1.5;
}
.pending-disabled-hint {
  font-size: 11px;
  color: var(--fg-muted);
}
.chk.disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.save-bar {
  position: sticky;
  bottom: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
  padding: 10px 12px;
  margin: 4px -6px -4px;
  background: var(--bg-elev);
  border-top: 1px solid transparent;
  border-radius: 6px;
}
.save-bar.dirty {
  justify-content: space-between;
  border: 1px solid color-mix(in srgb, var(--accent) 35%, var(--border));
  background: color-mix(in srgb, var(--accent) 8%, var(--paper));
}
.unsaved-msg {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--fg);
}
.unsaved-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--accent);
  flex-shrink: 0;
}
.save-bar-btns {
  display: flex;
  align-items: center;
  gap: 8px;
}
.adv-details {
  border-top: 1px solid var(--border);
  padding-top: 10px;
}
.adv-summary {
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-muted);
}
.adv-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 10px;
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
.btn.primary.dirty-btn {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 30%, transparent);
}
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 9000;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}
.modal-box {
  width: 100%;
  max-width: 440px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  box-shadow: 0 16px 40px rgba(0, 0, 0, 0.35);
  overflow: hidden;
}
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.modal-title {
  font-size: 13px;
  font-weight: 600;
}
.modal-close {
  background: transparent;
  border: none;
  color: var(--fg-muted);
  cursor: pointer;
  padding: 4px;
  display: flex;
}
.modal-body {
  padding: 14px 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 12.5px;
  line-height: 1.5;
}
.modal-body p {
  margin: 0;
}
.modal-sub {
  color: var(--fg-muted);
  font-size: 12px;
}
.modal-foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
}
</style>
