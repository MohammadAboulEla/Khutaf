<script setup>
import { shallowRef, computed } from "vue";
import AppHeader from "./components/AppHeader.vue";
import AppSidebar from "./components/AppSidebar.vue";
import AppFooter from "./components/AppFooter.vue";
import AboutModal from "./components/AboutModal.vue";
import HookConsoleView from "./views/HookConsoleView.vue";
import GamesView from "./views/GamesView.vue";
import EmbedSettingsView from "./views/EmbedSettingsView.vue";
import TranslatorsView from "./views/TranslatorsView.vue";
import TextPipelineView from "./views/TextPipelineView.vue";
import DebugView from "./views/DebugView.vue";
import { useTheme } from "./composables/useTheme.js";
import { useSidebar } from "./composables/useSidebar.js";
import { useSession } from "./composables/useSession.js";

const { theme, toggle: toggleTheme } = useTheme();
const {
  side: sidebar,
  collapsed: sidebarCollapsed,
  toggle: toggleSidebar,
  toggleCollapsed: toggleSidebarCollapsed,
} = useSidebar();

const { status, logs, errorBanner, clearError } = useSession();

const activeTab = shallowRef("games");
const aboutOpen = shallowRef(false);

const footerText = computed(() => {
  const last = logs.value[0];
  const base = status.value.attached
    ? `Attached: ${status.value.gameName} (${status.value.engine || "native"})`
    : "Idle — select a process to attach";
  const prov = status.value.debugMode
    ? status.value.debugRealTrans
      ? " · Translator: debugging (real)"
      : " · Translator: debugging (test mode)"
    : status.value.provider
      ? ` · Translator: ${status.value.provider}`
      : " · Translator: off";
  const logPart = last ? ` · [${last.time}] ${last.msg}` : "";
  return base + prov + logPart;
});
</script>

<template>
  <div class="app" :data-sidebar="sidebar">
    <AppHeader
      :theme="theme"
      :sidebar="sidebar"
      :host-ready="status.hostReady"
      @toggle-theme="toggleTheme"
      @toggle-sidebar="toggleSidebar"
      @open-about="aboutOpen = true"
    />

    <AppSidebar
      class="aside"
      :side="sidebar"
      :collapsed="sidebarCollapsed"
      :active="activeTab"
      @select="activeTab = $event"
      @toggle-collapsed="toggleSidebarCollapsed"
    />

    <main class="content">
      <div v-if="errorBanner" class="banner">
        <span>{{ errorBanner }}</span>
        <button class="banner-close" type="button" @click="clearError">×</button>
      </div>
      <GamesView v-if="activeTab === 'games'" @open-hooks="activeTab = 'hooks'" />
      <HookConsoleView v-show="activeTab === 'hooks'" />
      <EmbedSettingsView v-if="activeTab === 'embed'" />
      <TranslatorsView v-if="activeTab === 'translators'" />
      <TextPipelineView v-if="activeTab === 'pipeline'" />
      <DebugView v-if="activeTab === 'debug'" />
    </main>

    <AppFooter class="foot" :status="footerText" />

    <AboutModal :open="aboutOpen" @close="aboutOpen = false" />
  </div>
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
  margin: 10px 12px 0;
  padding: 6px 12px;
  background: color-mix(in oklab, #dc2626 20%, var(--bg-elev));
  border: 1px solid #dc2626;
  border-radius: 5px;
  color: #fca5a5;
  font-size: 12px;
}
.banner-close {
  background: transparent;
  border: none;
  color: inherit;
  font-size: 16px;
  cursor: pointer;
}
</style>
