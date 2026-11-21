<script setup>
import { ref, shallowRef } from "vue";
import Icon from "./Icon.vue";
import TranslatorBenchmark from "./TranslatorBenchmark.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { useUiMode } from "../composables/useUiMode.js";

const props = defineProps({
  open: { type: Boolean, default: false },
});

const emit = defineEmits(["close", "complete"]);

const { config, saveConfig, unityFontStatus, downloadUnityFonts } = useSession();
const { lang, isRtl, setLang, t } = useI18n();
const { uiMode, setUiMode } = useUiMode();

const step = shallowRef(1);
const fontLocation = shallowRef("appdata");
const TOTAL_STEPS = 4;

async function onPickProvider(providerId) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    translator: {
      ...config.value.translator,
      active: providerId,
    },
  });
}

function onFinish() {
  emit("complete", { language: lang.value, mode: uiMode.value });
}

function onSkip() {
  emit("complete", { language: lang.value, mode: uiMode.value });
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="onSkip">
        <div class="modal" role="dialog" aria-modal="true" :dir="isRtl ? 'rtl' : 'ltr'">
          <header class="head">
            <div>
              <div class="title">{{ t("wizTitle") }}</div>
              <div class="step-sub">{{ t("wizStepOf", { step, total: TOTAL_STEPS }) }}</div>
            </div>
            <button class="link-btn" type="button" @click="onSkip">{{ t("wizSkip") }}</button>
          </header>

          <div class="dots">
            <span v-for="n in TOTAL_STEPS" :key="n" class="dot" :class="{ active: n === step, done: n < step }" />
          </div>

          <div class="body">
            <template v-if="step === 1">
              <h3 class="h3">{{ t("wizS1Title") }}</h3>
              <p class="desc">{{ t("wizS1Desc") }}</p>

              <div class="pick-grid">
                <button
                  type="button"
                  class="pick-card"
                  :class="{ active: lang === 'ar' }"
                  @click="setLang('ar')"
                >
                  <div class="pick-title">{{ t("wizLangAr") }}</div>
                  <div class="pick-desc">{{ t("wizLangArDesc") }}</div>
                </button>
                <button
                  type="button"
                  class="pick-card"
                  :class="{ active: lang === 'en' }"
                  @click="setLang('en')"
                >
                  <div class="pick-title">{{ t("wizLangEn") }}</div>
                  <div class="pick-desc">{{ t("wizLangEnDesc") }}</div>
                </button>
              </div>

              <div class="pick-grid">
                <button
                  type="button"
                  class="pick-card"
                  :class="{ active: uiMode === 'simple' }"
                  @click="setUiMode('simple')"
                >
                  <div class="pick-title">{{ t("modeSimple") }}</div>
                  <div class="pick-desc">{{ t("wizModeSimpleDesc") }}</div>
                </button>
                <button
                  type="button"
                  class="pick-card"
                  :class="{ active: uiMode === 'advanced' }"
                  @click="setUiMode('advanced')"
                >
                  <div class="pick-title">{{ t("modeAdvanced") }}</div>
                  <div class="pick-desc">{{ t("wizModeAdvDesc") }}</div>
                </button>
              </div>
            </template>

            <template v-else-if="step === 2">
              <h3 class="h3">{{ t("wizS2Title") }}</h3>
              <p class="desc">{{ t("wizS2Desc") }}</p>

              <div v-if="unityFontStatus.installed" class="ok-box">
                <Icon name="CheckCircle" :size="18" />
                <div class="ok-text">
                  <div>{{ t("wizS2Installed", { count: unityFontStatus.count }) }}</div>
                  <div v-if="unityFontStatus.dir" class="path-hint" dir="ltr">{{ unityFontStatus.dir }}</div>
                </div>
              </div>

              <div v-else class="dl-box">
                <div class="loc-label">{{ t("wizS2LocLabel") }}</div>
                <div class="pick-grid full-w">
                  <button
                    type="button"
                    class="pick-card"
                    :class="{ active: fontLocation === 'appdata' }"
                    :disabled="unityFontStatus.downloading"
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
                    :disabled="unityFontStatus.downloading"
                    @click="fontLocation = 'app'"
                  >
                    <div class="pick-title">{{ t("wizS2LocAppTitle") }}</div>
                    <div class="pick-desc">{{ t("wizS2LocAppDesc") }}</div>
                    <div v-if="unityFontStatus.appDir" class="path-hint" dir="ltr">
                      {{ unityFontStatus.appDir }}
                    </div>
                  </button>
                </div>

                <button
                  class="btn primary"
                  type="button"
                  :disabled="unityFontStatus.downloading"
                  @click="downloadUnityFonts(fontLocation)"
                >
                  <Icon name="Download" :size="14" />
                  <span>
                    {{
                      unityFontStatus.downloading
                        ? t("embDownloading")
                        : t("embDownloadBtn")
                    }}
                  </span>
                </button>

                <div v-if="unityFontStatus.downloading || unityFontStatus.error" class="dl-prog">
                  <div v-if="unityFontStatus.downloading" class="progress-track">
                    <div class="progress-fill" :style="{ width: `${unityFontStatus.progress}%` }" />
                  </div>
                  <span class="sub" :class="{ err: unityFontStatus.error }">
                    {{ unityFontStatus.error || unityFontStatus.message }}
                  </span>
                </div>
              </div>

              <p class="note">{{ t("wizS2LaterNote") }}</p>
            </template>

            <template v-else-if="step === 3">
              <h3 class="h3">{{ t("wizS3Title") }}</h3>
              <p class="desc">{{ t("wizS3Desc") }}</p>
              <TranslatorBenchmark
                :compact="true"
                :translator-config="config?.translator"
                :active-provider="config?.translator?.active || 'bing'"
                @select-provider="onPickProvider"
              />
            </template>

            <template v-else>
              <h3 class="h3">{{ t("wizS4Title") }}</h3>
              <div class="guide-cards">
                <div class="guide-item">{{ t("wizS4G1") }}</div>
                <div class="guide-item">{{ t("wizS4G2") }}</div>
                <div class="guide-item">{{ t("wizS4G3") }}</div>
              </div>
            </template>
          </div>

          <footer class="foot">
            <button v-if="step > 1" class="btn" type="button" @click="step--">
              {{ t("wizBack") }}
            </button>
            <span v-else />
            <button v-if="step < TOTAL_STEPS" class="btn primary" type="button" @click="step++">
              {{ t("wizNext") }}
            </button>
            <button v-else class="btn primary" type="button" @click="onFinish">
              {{ t("wizFinish") }}
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
  z-index: 9500;
  background: rgba(0, 0, 0, 0.58);
  display: grid;
  place-items: center;
  padding: 20px;
}
.modal {
  width: min(680px, 100%);
  max-height: 90vh;
  display: grid;
  grid-template-rows: auto auto 1fr auto;
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.45);
  overflow: hidden;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
}
.title {
  font-size: 15px;
  font-weight: 700;
}
.step-sub {
  font-size: 11.5px;
  color: var(--fg-muted);
  margin-top: 2px;
}
.link-btn {
  background: none;
  border: none;
  color: var(--fg-muted);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.link-btn:hover {
  color: var(--accent);
}
.dots {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
  padding: 10px 18px 0;
}
.dot {
  height: 4px;
  border-radius: 2px;
  background: var(--border);
}
.dot.done {
  background: color-mix(in oklab, var(--accent) 55%, transparent);
}
.dot.active {
  background: var(--accent);
}
.body {
  padding: 16px 18px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.h3 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.desc {
  margin: 0;
  font-size: 12.5px;
  color: var(--fg-muted);
  line-height: 1.55;
}
.pick-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.full-w {
  width: 100%;
}
.loc-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg);
}
.ok-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.path-hint {
  margin-top: 6px;
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--fg-muted);
  word-break: break-all;
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
}
.pick-card.active {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--accent) 12%, var(--paper));
}
.pick-title {
  font-size: 13.5px;
  font-weight: 700;
  margin-bottom: 4px;
}
.pick-desc {
  font-size: 11.5px;
  color: var(--fg-muted);
  line-height: 1.45;
}
.ok-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px;
  background: color-mix(in oklab, #16a34a 15%, var(--paper));
  border: 1px solid rgba(74, 222, 128, 0.35);
  border-radius: 6px;
  color: #4ade80;
  font-size: 13px;
}
.dl-box {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 14px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.dl-prog {
  width: 100%;
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
}
.sub {
  font-size: 11.5px;
  color: var(--fg-muted);
}
.sub.err {
  color: #f87171;
}
.note {
  margin: 0;
  font-size: 11.5px;
  color: var(--fg-muted);
}
.guide-cards {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.guide-item {
  padding: 12px 14px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 13px;
  font-weight: 500;
}
.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 18px;
  border-top: 1px solid var(--border);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 14px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 6px;
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
