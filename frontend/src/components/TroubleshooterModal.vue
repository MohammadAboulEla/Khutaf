<script setup>
import { computed, onMounted, onUnmounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "open-translators"]);

const {
  config,
  saveConfig,
  saveState,
  unityFontStatus,
  downloadUnityFonts,
  processes,
  isAdmin,
  restartAsAdmin,
} = useSession();

const { t, isRtl } = useI18n();

const fontMsg = shallowRef("");
const wrapLen = shallowRef(40);
const fontScale = shallowRef(1.0);
const displayMode = shallowRef(0);

watch(
  config,
  (cfg) => {
    if (!cfg?.embed) return;
    wrapLen.value = cfg.embed.maxLineLength || 40;
    fontScale.value = cfg.embed.fontSizeRelative || 1.0;
    displayMode.value = cfg.embed.displayMode ?? 0;
  },
  { immediate: true },
);

const onKey = (e) => {
  if (e.key === "Escape" && props.open) emit("close");
};
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

const unsupportedSample = computed(() =>
  processes.value
    .filter((p) => !p.supported)
    .slice(0, 4)
    .map((p) => p.name)
    .join(", "),
);

async function onFixFonts() {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: {
      ...config.value.embed,
      changeFont: true,
      useUnityBundle: true,
    },
  });
  fontMsg.value = t("tsSquaresFixed");
}

async function onSaveDisplay() {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: {
      ...config.value.embed,
      displayMode: Number(displayMode.value),
      maxLineLength: Number(wrapLen.value) || 40,
      fontSizeRelative: Number(fontScale.value) || 1.0,
    },
  });
}

function onOpenBench() {
  emit("close");
  emit("open-translators");
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="emit('close')">
        <div class="modal" role="dialog" aria-modal="true" :dir="isRtl ? 'rtl' : 'ltr'">
          <header class="head">
            <div class="title">{{ t("tsTitle") }}</div>
            <button class="close" type="button" @click="emit('close')">
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <p class="intro">{{ t("tsIntro") }}</p>

            <div class="issue-card">
              <div class="issue-title">{{ t("tsSquaresTitle") }}</div>
              <p class="issue-desc">{{ t("tsSquaresDesc") }}</p>
              <div class="issue-actions">
                <button class="btn primary" type="button" @click="onFixFonts">
                  {{ t("tsSquaresFixBtn") }}
                </button>
                <button
                  v-if="!unityFontStatus.installed"
                  class="btn"
                  type="button"
                  :disabled="unityFontStatus.downloading"
                  @click="downloadUnityFonts"
                >
                  {{ unityFontStatus.downloading ? t("embDownloading") : t("embDownloadBtn") }}
                </button>
              </div>
              <div v-if="fontMsg" class="ok-msg">{{ fontMsg }}</div>
            </div>

            <div class="issue-card">
              <div class="issue-title">{{ t("tsNotListedTitle") }}</div>
              <p class="issue-desc">{{ t("tsNotListedDesc") }}</p>
              <div v-if="unsupportedSample" class="sub-note">
                {{ t("tsNonUnityNote", { names: unsupportedSample }) }}
              </div>
              <div class="issue-actions">
                <span v-if="isAdmin" class="ok-badge">{{ t("tsAdminOk") }}</span>
                <button v-else class="btn primary" type="button" @click="restartAsAdmin">
                  <Icon name="Shield" :size="13" />
                  <span>{{ t("tsAdminBtn") }}</span>
                </button>
              </div>
            </div>

            <div class="issue-card">
              <div class="issue-title">{{ t("tsSlowTitle") }}</div>
              <p class="issue-desc">{{ t("tsSlowDesc") }}</p>
              <div class="issue-actions">
                <button class="btn primary" type="button" @click="onOpenBench">
                  <Icon name="Zap" :size="13" />
                  <span>{{ t("tsSlowBtn") }}</span>
                </button>
              </div>
            </div>

            <div class="issue-card">
              <div class="issue-title">{{ t("tsWrapTitle") }}</div>
              <p class="issue-desc">{{ t("tsWrapDesc") }}</p>
              <div class="inline-grid">
                <label class="field">
                  <span class="lbl">{{ t("embDisplayMode") }}</span>
                  <select v-model.number="displayMode" class="input">
                    <option :value="0">{{ t("qpDisplayTransOnly") }}</option>
                    <option :value="1">{{ t("qpDisplayOriTrans") }}</option>
                    <option :value="2">{{ t("embOptTransOri") }}</option>
                    <option :value="3">{{ t("qpDisplayOriOnly") }}</option>
                  </select>
                </label>
                <label class="field">
                  <span class="lbl">{{ t("embLineLen") }}</span>
                  <input v-model.number="wrapLen" type="number" min="15" max="120" class="input" />
                </label>
                <label class="field">
                  <span class="lbl">{{ t("embFontScale") }}</span>
                  <input v-model.number="fontScale" type="number" min="0.5" max="2.5" step="0.05" class="input" />
                </label>
              </div>
              <div class="issue-actions">
                <button class="btn primary" type="button" @click="onSaveDisplay">
                  {{ saveState === "saved" ? t("trSavedBtn") : t("embApplySave") }}
                </button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  z-index: 9200;
  background: rgba(0, 0, 0, 0.52);
  display: grid;
  place-items: center;
  padding: 20px;
}
.modal {
  width: min(640px, 100%);
  max-height: 88vh;
  display: grid;
  grid-template-rows: auto 1fr;
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.42);
  overflow: hidden;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.title {
  font-size: 14.5px;
  font-weight: 700;
}
.close {
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg);
  cursor: pointer;
}
.close:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
.body {
  padding: 14px 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.intro {
  margin: 0;
  font-size: 12.5px;
  color: var(--fg-muted);
}
.issue-card {
  padding: 12px 14px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 7px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.issue-title {
  font-size: 13px;
  font-weight: 700;
}
.issue-desc {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
  line-height: 1.5;
}
.sub-note {
  font-size: 11px;
  color: var(--fg-muted);
  font-family: var(--mono);
}
.issue-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.ok-msg {
  font-size: 11.5px;
  color: #4ade80;
}
.ok-badge {
  padding: 3px 8px;
  border-radius: 4px;
  background: color-mix(in oklab, #16a34a 18%, transparent);
  color: #4ade80;
  font-size: 12px;
}
.inline-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
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
.input {
  height: 30px;
  padding: 0 8px;
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  min-width: 0;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
</style>
