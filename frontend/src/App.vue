<script setup>
import { shallowRef, computed, watch, onMounted, onBeforeUnmount } from "vue";
import AppHeader from "./components/AppHeader.vue";
import AppSidebar from "./components/AppSidebar.vue";
import AppFooter from "./components/AppFooter.vue";
import AboutModal from "./components/AboutModal.vue";
import UpdateModal from "./components/UpdateModal.vue";
import FontDownloadModal from "./components/FontDownloadModal.vue";
import OnboardingWizard from "./components/OnboardingWizard.vue";
import TroubleshooterModal from "./components/TroubleshooterModal.vue";
import QuickHooksModal from "./components/QuickHooksModal.vue";
import QuickPlayView from "./views/QuickPlayView.vue";
import HookConsoleView from "./views/HookConsoleView.vue";
import GamesView from "./views/GamesView.vue";
import EmbedSettingsView from "./views/EmbedSettingsView.vue";
import HotkeysView from "./views/HotkeysView.vue";
import TranslatorsView from "./views/TranslatorsView.vue";
import TextPipelineView from "./views/TextPipelineView.vue";
import DebugView from "./views/DebugView.vue";
import { useTheme } from "./composables/useTheme.js";
import { useSidebar } from "./composables/useSidebar.js";
import { useSession } from "./composables/useSession.js";
import { useI18n } from "./composables/useI18n.js";
import { useUiMode } from "./composables/useUiMode.js";
import { formatFriendlyError } from "./utils/friendlyErrors.js";

const { theme, toggle: toggleTheme } = useTheme();
const {
  side: sidebar,
  collapsed: sidebarCollapsed,
  toggle: toggleSidebar,
  toggleCollapsed: toggleSidebarCollapsed,
} = useSidebar();

const { lang, t, toggleLang, setLang, syncFromConfig: syncLang } = useI18n();
const { uiMode, isSimple, toggleUiMode, setUiMode, syncFromConfig: syncMode } = useUiMode();

const {
  status,
  hooks,
  logs,
  config,
  saveConfig,
  unityFontStatus,
  updateStatus,
  errorBanner,
  clearError,
  appVersion,
  pipelineTarget,
  restartAsAdmin,
  fontDownloadPromptOpen,
  openFontDownloadPrompt,
  closeFontDownloadPrompt,
  quickHooksOpen,
  quickHooksStandalone,
  setQuickHooksOpen,
  hotkeyToast,
  hideToTray,
  quitApp,
  checkForUpdates,
  downloadUpdate,
  cancelUpdateDownload,
  installUpdate,
  skipUpdateVersion,
  openReleasePage,
} = useSession();

const embeddableHooksCount = computed(() => hooks.value.filter((h) => h.embeddable).length);

function onCloseWindow() {
  if (config.value?.hotkeys?.minimizeToTray) {
    hideToTray();
  } else {
    quitApp();
  }
}

const activeTab = shallowRef(isSimple.value ? "quickplay" : "games");
const aboutOpen = shallowRef(false);
const updateModalOpen = shallowRef(false);
const updatedToast = shallowRef("");
const wizardOpen = shallowRef(false);
const troubleshooterOpen = shallowRef(false);
let configSynced = false;
let updatedToastShown = false;
let updatedToastTimer = null;

watch(
  () => updateStatus.value?.updatedFrom,
  (from) => {
    if (!from || updatedToastShown) return;
    updatedToastShown = true;
    const ver = appVersion.value || updateStatus.value?.currentVersion || "";
    updatedToast.value = t("updToastUpdated", { version: ver });
    if (updatedToastTimer) clearTimeout(updatedToastTimer);
    updatedToastTimer = setTimeout(() => {
      updatedToast.value = "";
      updatedToastTimer = null;
    }, 4500);
  },
  { immediate: true },
);

async function onToggleAutoCheck(enabled) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    update: {
      ...(config.value.update || {}),
      autoCheck: Boolean(enabled),
    },
  });
}

async function onDownloadUpdate() {
  try {
    await downloadUpdate();
  } catch {
    // Error state is emitted via updater:status.
  }
}

async function onInstallUpdate(detach) {
  try {
    await installUpdate(detach);
  } catch {
    // Error state is emitted via updater:status.
  }
}

watch(
  config,
  (cfg) => {
    if (!cfg || configSynced) return;
    configSynced = true;
    syncLang(cfg.ui);
    syncMode(cfg.ui);
    if (cfg.ui?.mode === "advanced") {
      activeTab.value = "games";
    } else {
      activeTab.value = "quickplay";
    }
    const alreadyLocal =
      typeof localStorage !== "undefined" && localStorage.getItem("khutaf.onboarded") === "1";
    if (!cfg.ui?.onboarded && !alreadyLocal) {
      wizardOpen.value = true;
    }
  },
  { immediate: true },
);

watch(isSimple, (simple) => {
  if (simple && !["quickplay", "translators", "embed", "hotkeys"].includes(activeTab.value)) {
    activeTab.value = "quickplay";
  }
});

function persistUI(patch) {
  if (!config.value) return;
  saveConfig({
    ...config.value,
    ui: {
      language: config.value.ui?.language || lang.value,
      mode: config.value.ui?.mode || uiMode.value,
      onboarded: Boolean(config.value.ui?.onboarded),
      ...patch,
    },
  });
}

function onToggleLang() {
  toggleLang();
  persistUI({ language: lang.value });
}

function onToggleMode() {
  toggleUiMode();
  persistUI({ mode: uiMode.value });
}

function onWizardComplete(payload) {
  wizardOpen.value = false;
  try {
    localStorage.setItem("khutaf.onboarded", "1");
  } catch {
    // ignore
  }
  const nextLang = payload?.language || lang.value;
  const nextMode = payload?.mode || uiMode.value;
  setLang(nextLang);
  setUiMode(nextMode);
  persistUI({ language: nextLang, mode: nextMode, onboarded: true });
  activeTab.value = nextMode === "simple" ? "quickplay" : "games";
}

function onEditGameRules(exePath) {
  pipelineTarget.value = exePath;
  activeTab.value = "pipeline";
}

const formattedError = computed(() => formatFriendlyError(errorBanner.value, lang.value));

async function onErrorAction() {
  const act = formattedError.value.action;
  if (act === "admin") {
    await restartAsAdmin();
  } else if (act === "benchmark") {
    clearError();
    activeTab.value = "translators";
  } else if (act === "fonts") {
    clearError();
    openFontDownloadPrompt();
  }
}

const footerText = computed(() => {
  const last = logs.value[0];
  const base = status.value.attached
    ? t("statusAttached", { game: status.value.gameName, engine: status.value.engine || "native" })
    : t("statusIdle");
  const prov = status.value.debugMode
    ? status.value.debugRealTrans
      ? ` · ${t("statusDebugReal")}`
      : ` · ${t("statusDebugMock")}`
    : status.value.provider
      ? ` · ${t("statusTranslator", { provider: status.value.provider })}`
      : ` · ${t("statusTranslatorOff")}`;
  const pausedPart = status.value.paused ? ` · ⏸ ${t("statusPaused")}` : "";
  const logPart = last ? ` · [${last.time}] ${last.msg}` : "";
  return base + prov + pausedPart + logPart;
});

function onWindowFocus() {
  if (!quickHooksStandalone.value) {
    window.go?.main?.App?.EnsureMainWindowGeometry?.();
  }
}

onMounted(() => window.addEventListener("focus", onWindowFocus));
onBeforeUnmount(() => window.removeEventListener("focus", onWindowFocus));
</script>

<template>
  <QuickHooksModal
    v-if="quickHooksOpen && quickHooksStandalone"
    :open="true"
    :standalone="true"
    @close="setQuickHooksOpen(false)"
  />
  <div v-else class="app" :data-sidebar="sidebar">
    <AppHeader
      :theme="theme"
      :sidebar="sidebar"
      :host-ready="status.hostReady"
      :attached="status.attached"
      :hooks-count="embeddableHooksCount"
      :hooks-hotkey="config?.hotkeys?.toggleHooksMenu || 'Ctrl+Alt+H'"
      :version="appVersion"
      :update-status="updateStatus"
      @toggle-theme="toggleTheme"
      @toggle-sidebar="toggleSidebar"
      @open-about="aboutOpen = true"
      @open-troubleshooter="troubleshooterOpen = true"
      @open-quick-hooks="setQuickHooksOpen(true)"
      @open-update="updateModalOpen = true"
      @toggle-mode="onToggleMode"
      @toggle-lang="onToggleLang"
      @close-window="onCloseWindow"
    />

    <AppSidebar
      class="aside"
      :side="sidebar"
      :collapsed="sidebarCollapsed"
      :active="activeTab"
      :simple="isSimple"
      @select="activeTab = $event"
      @toggle-collapsed="toggleSidebarCollapsed"
    />

    <main class="content">
      <div v-if="errorBanner" class="banner">
        <div class="banner-body">
          <strong>{{ formattedError.title }}</strong>
          <span v-if="formattedError.hint" class="banner-hint">{{ formattedError.hint }}</span>
        </div>
        <div class="banner-actions">
          <button
            v-if="formattedError.action === 'admin'"
            class="banner-act"
            type="button"
            @click="onErrorAction"
          >
            {{ t("errActionAdmin") }}
          </button>
          <button
            v-else-if="formattedError.action === 'benchmark'"
            class="banner-act"
            type="button"
            @click="onErrorAction"
          >
            {{ t("errActionBench") }}
          </button>
          <button
            v-else-if="formattedError.action === 'fonts'"
            class="banner-act"
            type="button"
            @click="onErrorAction"
          >
            {{ t("errActionFonts") }}
          </button>
          <button class="banner-close" type="button" @click="clearError">×</button>
        </div>
      </div>

      <QuickPlayView
        v-if="activeTab === 'quickplay'"
        @open-translators="activeTab = 'translators'"
        @open-troubleshooter="troubleshooterOpen = true"
        @open-wizard="wizardOpen = true"
      />
      <GamesView
        v-if="activeTab === 'games'"
        @open-hooks="activeTab = 'hooks'"
        @edit-rules="onEditGameRules"
      />
      <HookConsoleView v-show="activeTab === 'hooks'" :active="activeTab === 'hooks'" />
      <EmbedSettingsView
        v-if="activeTab === 'embed'"
        :onboarding-open="wizardOpen"
      />
      <HotkeysView v-if="activeTab === 'hotkeys'" />
      <TranslatorsView v-if="activeTab === 'translators'" />
      <TextPipelineView v-if="activeTab === 'pipeline'" />
      <DebugView v-if="activeTab === 'debug'" />
    </main>

    <AppFooter class="foot" :status="footerText" :version="appVersion" />

    <AboutModal
      :open="aboutOpen"
      :version="appVersion"
      :update-status="updateStatus"
      :auto-check-updates="config?.update?.autoCheck !== false"
      @close="aboutOpen = false"
      @check-updates="checkForUpdates(true)"
      @open-update="aboutOpen = false; updateModalOpen = true"
      @toggle-auto-check="onToggleAutoCheck"
    />
    <UpdateModal
      :open="updateModalOpen"
      :status="updateStatus"
      :attached="status.attached"
      @close="updateModalOpen = false"
      @download="onDownloadUpdate"
      @cancel-download="cancelUpdateDownload"
      @install="onInstallUpdate"
      @skip="skipUpdateVersion"
      @open-github="openReleasePage"
      @restart-admin="restartAsAdmin"
    />
    <FontDownloadModal :open="fontDownloadPromptOpen" @close="closeFontDownloadPrompt" />
    <OnboardingWizard :open="wizardOpen" @complete="onWizardComplete" @close="wizardOpen = false" />
    <TroubleshooterModal
      :open="troubleshooterOpen"
      @close="troubleshooterOpen = false"
      @open-translators="activeTab = 'translators'"
    />
    <QuickHooksModal v-if="!quickHooksStandalone" :open="quickHooksOpen" @close="setQuickHooksOpen(false)" />
  </div>

  <Transition name="fade">
    <div v-if="hotkeyToast" class="hotkey-toast" :class="{ warn: hotkeyToast.action === 'warning' }">
      {{ hotkeyToast.label }}
    </div>
    <div v-else-if="updatedToast" class="hotkey-toast">
      {{ updatedToast }}
    </div>
  </Transition>
</template>

<style scoped>
.app {
  height: 100%;
  display: grid;
  grid-template-rows: auto 1fr auto;
  grid-template-columns: auto 1fr;
  grid-template-areas:
    "header header"
    "aside  content"
    "foot   foot";
}
.app[data-sidebar="right"] {
  grid-template-columns: 1fr auto;
  grid-template-areas:
    "header  header"
    "content aside"
    "foot    foot";
}
.app > :first-child {
  grid-area: header;
}
.aside {
  grid-area: aside;
}
.content {
  grid-area: content;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.foot {
  grid-area: foot;
}
.banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  margin: 10px 12px 0;
  padding: 8px 12px;
  background: color-mix(in oklab, #dc2626 20%, var(--bg-elev));
  border: 1px solid #dc2626;
  border-radius: 6px;
  color: #fecaca;
  font-size: 12px;
}
.banner-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.banner-hint {
  font-size: 11px;
  opacity: 0.9;
}
.banner-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.banner-act {
  height: 24px;
  padding: 0 10px;
  border-radius: 4px;
  border: 1px solid rgba(255, 255, 255, 0.35);
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}
.banner-close {
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  cursor: pointer;
}
.hotkey-toast {
  position: fixed;
  bottom: 36px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 9500;
  padding: 7px 14px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--accent) 92%, #000);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  pointer-events: none;
}
.hotkey-toast.warn {
  background: #b91c1c;
}
</style>
