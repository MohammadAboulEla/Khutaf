<script setup>
import { computed, onMounted, onUnmounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useI18n } from "../composables/useI18n.js";
import { useSession } from "../composables/useSession.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);

const { unityFontStatus, downloadUnityFonts } = useSession();
const { t, isRtl } = useI18n();

const fontLocation = shallowRef("appdata");

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      fontLocation.value = unityFontStatus.value?.location || "appdata";
    }
  },
);

const onKey = (e) => {
  if (e.key === "Escape" && props.open) emit("close");
};
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

const isDownloading = computed(() => Boolean(unityFontStatus.value?.downloading));

function onStartDownload() {
  const chosen = fontLocation.value;
  emit("close");
  downloadUnityFonts(chosen);
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="emit('close')">
        <div
          class="modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('fontModalTitle')"
          :dir="isRtl ? 'rtl' : 'ltr'"
        >
          <header class="head">
            <div class="title-wrap">
              <span class="title-ico">
                <Icon name="Download" :size="15" />
              </span>
              <span class="title">{{ t("fontModalTitle") }}</span>
            </div>
            <button class="close" type="button" @click="emit('close')" :aria-label="t('fontModalCancel')">
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <div class="loc-label">{{ t("wizS2LocLabel") }}</div>

            <div class="pick-grid">
              <button
                type="button"
                class="pick-card"
                :class="{ active: fontLocation === 'appdata' }"
                :disabled="isDownloading"
                @click="fontLocation = 'appdata'"
              >
                <div class="pick-title">{{ t("wizS2LocAppDataTitle") }}</div>
                <div class="pick-desc">{{ t("wizS2LocAppDataDesc") }}</div>
                <div v-if="unityFontStatus.appDataDir" class="path-hint" dir="ltr">
                  {{ unityFontStatus.appDataDir }}
                </div>
              </button>

              <button
                type="button"
                class="pick-card"
                :class="{ active: fontLocation === 'app' }"
                :disabled="isDownloading"
                @click="fontLocation = 'app'"
              >
                <div class="pick-title">{{ t("wizS2LocAppTitle") }}</div>
                <div class="pick-desc">{{ t("wizS2LocAppDesc") }}</div>
                <div v-if="unityFontStatus.appDir" class="path-hint" dir="ltr">
                  {{ unityFontStatus.appDir }}
                </div>
              </button>
            </div>

            <div class="info-note">
              <Icon name="Info" :size="14" />
              <span>{{ t("wizS2LocBothChecked") }}</span>
            </div>
          </div>

          <footer class="foot">
            <button class="btn" type="button" @click="emit('close')">
              {{ t("fontModalCancel") }}
            </button>
            <button
              class="btn primary"
              type="button"
              :disabled="isDownloading"
              @click="onStartDownload"
            >
              <Icon name="Download" :size="13" />
              <span>{{ isDownloading ? t("embDownloading") : t("embDownloadBtn") }}</span>
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: grid;
  place-items: center;
  z-index: 1040;
  padding: 24px;
}
.modal {
  display: grid;
  grid-template-rows: auto 1fr auto;
  width: min(540px, 100%);
  max-height: min(560px, 90vh);
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border-input, var(--border));
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
  overflow: hidden;
}
.modal[dir="rtl"] {
  font-family: var(--arabic);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}
.title-ico {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
}
.title {
  font-weight: 600;
  font-size: 14px;
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
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.loc-label {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
}
.pick-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
@media (max-width: 500px) {
  .pick-grid {
    grid-template-columns: 1fr;
  }
}
.pick-card {
  text-align: start;
  padding: 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 8px;
  font: inherit;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.pick-card:hover:not(:disabled) {
  border-color: color-mix(in oklab, var(--accent) 45%, var(--border));
}
.pick-card.active {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--accent) 12%, var(--paper));
}
.pick-title {
  font-size: 13px;
  font-weight: 700;
}
.pick-desc {
  font-size: 11.5px;
  color: var(--fg-muted);
  line-height: 1.5;
}
.path-hint {
  margin-top: 4px;
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--fg-faint);
  word-break: break-all;
}
.info-note {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 11px;
  border-radius: 8px;
  background: var(--bg-pill);
  border: 1px solid var(--border);
  font-size: var(--fs-small);
  line-height: 1.5;
  color: var(--fg-muted);
}
.info-note > svg {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--accent);
}
.foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 13px;
  border-radius: 6px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: var(--fs-small);
  font-weight: 500;
  cursor: pointer;
}
.btn:hover:not(:disabled) {
  background: var(--bg-hover);
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.btn.primary:hover:not(:disabled) {
  opacity: 0.92;
}
.btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.12s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
