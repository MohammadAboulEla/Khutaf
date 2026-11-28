<script setup>
import { onMounted, onUnmounted, reactive, ref, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import Tooltip from "./Tooltip.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { ALL_PROVIDERS } from "../utils/providers.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  game: { type: Object, default: null },
});

const emit = defineEmits(["close"]);

const { config, listFonts, unityFontStatus, saveGameOverrides, saveState } = useSession();
const { t, isRtl } = useI18n();

const fonts = shallowRef(["Tahoma", "Arial", "Segoe UI"]);
const useGlobalEmbed = shallowRef(true);
const useGlobalTranslator = shallowRef(true);
const errMsg = ref("");

const embedForm = reactive({
  timeoutMs: 3000,
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

const translatorForm = reactive({
  active: "bing",
  fallbackProvider: "",
  bing: { sourceLang: "auto-detect", targetLang: "ar" },
  traditional: { sourceLang: "auto", targetLang: "ar" },
  llm: { baseUrl: "", apiKey: "", model: "", systemPrompt: "", temperature: 0.2, sourceLang: "English", targetLang: "Arabic" },
  offlineLlm: { baseUrl: "http://127.0.0.1:8080/v1", model: "local-model", template: "auto", sourceLang: "English", targetLang: "Arabic", temperature: 0.1 },
  llamaCpp: { binaryPath: "", modelPath: "", port: 8080, gpuLayers: 99, ctxSize: 4096, autoStart: false, extraArgs: "" },
});

function syncFromProps() {
  errMsg.value = "";
  const g = props.game;
  const globalCfg = config.value;
  if (!g || !globalCfg) return;

  useGlobalEmbed.value = !g.embedOverride;
  Object.assign(embedForm, globalCfg.embed, g.embedOverride || {});

  useGlobalTranslator.value = !g.translatorOverride;
  const srcTr = g.translatorOverride || globalCfg.translator;
  translatorForm.active = srcTr.active || "bing";
  translatorForm.fallbackProvider = srcTr.fallbackProvider || "";
  translatorForm.bing = { ...(globalCfg.translator.bing || {}), ...(srcTr.bing || {}) };
  translatorForm.traditional = { ...(globalCfg.translator.traditional || {}), ...(srcTr.traditional || {}) };
  translatorForm.llm = { ...(globalCfg.translator.llm || {}), ...(srcTr.llm || {}) };
  translatorForm.offlineLlm = { ...(globalCfg.translator.offlineLlm || {}), ...(srcTr.offlineLlm || {}) };
  translatorForm.llamaCpp = { ...(globalCfg.translator.llamaCpp || {}), ...(srcTr.llamaCpp || {}) };
}

watch([() => props.open, () => props.game], ([isOpen]) => {
  if (isOpen) syncFromProps();
}, { immediate: true });

const onKey = (e) => {
  if (e.key === "Escape" && props.open) emit("close");
};

onMounted(async () => {
  window.addEventListener("keydown", onKey);
  const list = await listFonts();
  if (list?.length) fonts.value = list;
});
onUnmounted(() => window.removeEventListener("keydown", onKey));

async function onSave() {
  if (!props.game) return;
  errMsg.value = "";
  const eo = useGlobalEmbed.value
    ? null
    : {
        timeoutMs: Number(embedForm.timeoutMs) || 3000,
        displayMode: Number(embedForm.displayMode),
        clearText: Boolean(embedForm.clearText),
        changeFont: Boolean(embedForm.changeFont),
        fontFamily: (embedForm.fontFamily || "Tahoma").trim(),
        useUnityBundle: Boolean(embedForm.useUnityBundle),
        changeFontSize: Boolean(embedForm.changeFontSize),
        fontSizeRelative: Number(embedForm.fontSizeRelative) || 1.0,
        limitLineLength: Boolean(embedForm.limitLineLength),
        maxLineLength: Number(embedForm.maxLineLength) || 40,
        stripTypewriterTags: Boolean(embedForm.stripTypewriterTags),
        nonBlocking: Boolean(embedForm.nonBlocking),
        pendingText: (embedForm.pendingText || "").trim(),
        verboseHooks: Boolean(embedForm.verboseHooks),
      };
  const fb = translatorForm.fallbackProvider === translatorForm.active ? "" : translatorForm.fallbackProvider;
  const to = useGlobalTranslator.value
    ? null
    : {
        active: translatorForm.active,
        fallbackProvider: fb,
        bing: { ...translatorForm.bing },
        traditional: { ...translatorForm.traditional },
        llm: { ...translatorForm.llm },
        offlineLlm: { ...translatorForm.offlineLlm },
        llamaCpp: { ...translatorForm.llamaCpp },
      };
  try {
    await saveGameOverrides(props.game.exePath, eo, to);
    emit("close");
  } catch (e) {
    errMsg.value = String(e);
  }
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open && game" class="overlay" @click.self="emit('close')">
        <div class="modal" role="dialog" aria-modal="true" :dir="isRtl ? 'rtl' : 'ltr'">
          <header class="head">
            <div class="title">{{ t("goModalTitle", { game: game.title }) }}</div>
            <button class="close" type="button" @click="emit('close')">
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <section class="section">
              <div class="sec-head">
                <h3 class="h3">{{ t("goSectionEmbed") }}</h3>
                <label class="chk">
                  <input v-model="useGlobalEmbed" type="checkbox" />
                  <span>{{ t("goInheritEmbed") }}</span>
                </label>
              </div>

              <div v-if="!useGlobalEmbed" class="grid">
                <label class="field">
                  <span class="lbl">{{ t("embDisplayMode") }}</span>
                  <select v-model.number="embedForm.displayMode" class="input">
                    <option :value="0">{{ t("embOptTransOnly") }}</option>
                    <option :value="1">{{ t("embOptOriTrans") }}</option>
                    <option :value="2">{{ t("embOptTransOri") }}</option>
                    <option :value="3">{{ t("embOptOriOnly") }}</option>
                  </select>
                </label>

                <label class="field">
                  <span class="lbl">{{ t("embLineLen") }}</span>
                  <input v-model.number="embedForm.maxLineLength" type="number" min="15" max="120" class="input" />
                </label>

                <label class="field" :class="{ disabled: !embedForm.changeFont }">
                  <span class="lbl">{{ t("embFontFamily") }}</span>
                  <select v-model="embedForm.fontFamily" class="input" :disabled="!embedForm.changeFont">
                    <option v-if="embedForm.fontFamily && !fonts.includes(embedForm.fontFamily)" :value="embedForm.fontFamily">
                      {{ embedForm.fontFamily }}
                    </option>
                    <option v-for="f in fonts" :key="f" :value="f">{{ f }}</option>
                  </select>
                  <span class="field-hint">{{ t("embFontFamilyHint") }}</span>
                </label>

                <label class="field">
                  <span class="lbl">{{ t("embFontScale") }}</span>
                  <input v-model.number="embedForm.fontSizeRelative" type="number" min="0.5" max="2.5" step="0.05" class="input" />
                </label>

                <label class="field">
                  <span class="lbl">{{ t("embTimeout") }}</span>
                  <input v-model.number="embedForm.timeoutMs" type="number" min="200" max="15000" step="100" class="input" />
                </label>

                <div class="checks">
                  <label class="chk">
                    <input v-model="embedForm.changeFont" type="checkbox" />
                    <span>{{ t("embChangeFont") }}</span>
                  </label>
                  <label class="chk" :class="{ disabled: !embedForm.changeFont || !unityFontStatus.installed }">
                    <input
                      v-model="embedForm.useUnityBundle"
                      type="checkbox"
                      :disabled="!embedForm.changeFont || !unityFontStatus.installed"
                    />
                    <span>{{ t("embUseBundleChk") }}</span>
                  </label>
                  <label class="chk">
                    <input v-model="embedForm.changeFontSize" type="checkbox" />
                    <span>{{ t("embChangeScale") }}</span>
                  </label>
                  <label class="chk">
                    <input v-model="embedForm.nonBlocking" type="checkbox" />
                    <span>{{ t("embNonBlocking") }}</span>
                  </label>
                  <Tooltip :text="t('verboseHooksTip')">
                    <label class="chk">
                      <input v-model="embedForm.verboseHooks" type="checkbox" />
                      <span>{{ t("verboseHooksLabel") }}</span>
                    </label>
                  </Tooltip>
                </div>
              </div>
            </section>

            <section class="section">
              <div class="sec-head">
                <h3 class="h3">{{ t("goSectionTranslator") }}</h3>
                <label class="chk">
                  <input v-model="useGlobalTranslator" type="checkbox" />
                  <span>{{ t("goInheritTranslator") }}</span>
                </label>
              </div>

              <div v-if="!useGlobalTranslator" class="grid">
                <label class="field">
                  <span class="lbl">{{ t("wizS3PrimaryLbl") }}</span>
                  <select v-model="translatorForm.active" class="input">
                    <option v-for="p in ALL_PROVIDERS" :key="p.id" :value="p.id">{{ p.label }}</option>
                    <option value="none">{{ t("trDisabled") }}</option>
                  </select>
                </label>

                <label class="field">
                  <span class="lbl">{{ t("wizS3FallbackLbl") }}</span>
                  <select v-model="translatorForm.fallbackProvider" class="input">
                    <option value="">{{ t("trFallbackNone") }}</option>
                    <option
                      v-for="p in ALL_PROVIDERS.filter((x) => x.id !== translatorForm.active)"
                      :key="p.id"
                      :value="p.id"
                    >
                      {{ p.label }}
                    </option>
                  </select>
                </label>
              </div>
            </section>

            <div v-if="errMsg" class="err">{{ errMsg }}</div>
          </div>

          <footer class="foot">
            <button class="btn" type="button" @click="emit('close')">{{ t("benchCancel") }}</button>
            <button class="btn primary" type="button" :disabled="saveState === 'saving'" @click="onSave">
              {{ saveState === "saving" ? t("embSaving") : t("embApplySave") }}
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
  z-index: 9200;
  background: rgba(0, 0, 0, 0.55);
  display: grid;
  place-items: center;
  padding: 20px;
}
.modal {
  width: min(640px, 100%);
  max-height: 88vh;
  display: grid;
  grid-template-rows: auto 1fr auto;
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
.body {
  padding: 14px 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.section {
  padding: 12px 14px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 7px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.h3 {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
}
.grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}
.checks {
  grid-column: 1 / -1;
  display: flex;
  flex-wrap: wrap;
  gap: 10px 18px;
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
.field-hint {
  font-size: 11px;
  color: var(--fg-muted);
  line-height: 1.4;
}
.input {
  height: 30px;
  padding: 0 10px;
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  cursor: pointer;
}
.field.disabled,
.chk.disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.err {
  color: #f87171;
  font-size: 12px;
}
.foot {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 16px;
  border-top: 1px solid var(--border);
}
.btn {
  height: 30px;
  padding: 0 14px;
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
.btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
</style>
