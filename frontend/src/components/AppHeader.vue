<script setup>
import { computed } from "vue";
import Icon from "./Icon.vue";
import Tooltip from "./Tooltip.vue";
import { useI18n } from "../composables/useI18n.js";
import { useUiMode } from "../composables/useUiMode.js";

const props = defineProps({
  version: { type: String, default: "0.1.0" },
  theme: { type: String, default: "dark" },
  sidebar: { type: String, default: "left" },
  hostReady: { type: Boolean, default: false },
  attached: { type: Boolean, default: false },
  hooksCount: { type: Number, default: 0 },
  hooksHotkey: { type: String, default: "Ctrl+Alt+H" },
  updateStatus: { type: Object, default: () => ({ state: "idle" }) },
});

const emit = defineEmits([
  "toggle-theme",
  "toggle-sidebar",
  "open-about",
  "open-troubleshooter",
  "open-quick-hooks",
  "open-update",
  "toggle-mode",
  "toggle-lang",
  "close-window",
]);

const { t } = useI18n();
const { isSimple } = useUiMode();

const updateState = computed(() => props.updateStatus?.state || "idle");
const showUpdatePill = computed(() =>
  ["available", "downloading", "verifying", "ready"].includes(updateState.value),
);
const updatePillLabel = computed(() => {
  const st = updateState.value;
  if (st === "ready") return t("updHeaderReady");
  if (st === "downloading" || st === "verifying") {
    return t("updHeaderDownloading", { pct: props.updateStatus?.progress ?? 0 });
  }
  return t("updHeaderAvailable", { version: props.updateStatus?.latestVersion || "" });
});
const updatePillTip = computed(() => {
  const st = updateState.value;
  if (st === "ready") return t("updHeaderReadyTip");
  if (st === "downloading" || st === "verifying") {
    return t("updHeaderDownloadingTip", { pct: props.updateStatus?.progress ?? 0 });
  }
  return t("updHeaderAvailableTip", { version: props.updateStatus?.latestVersion || "" });
});

const wails = () => window.runtime || (window.go && window.runtime);
const minimize = () => wails()?.WindowMinimise();
const toggleMax = () => wails()?.WindowToggleMaximise();
const close = () => emit("close-window");
</script>

<template>
  <header class="app-header drag">
    <div class="brand">
      <div class="brand-mark">Kh</div>
      <div class="ar">{{ t("appTitle") }}</div>
      <div class="en">{{ t("appSubtitle") }} · v{{ version }}</div>
    </div>

    <div class="crumbs">
      <span class="dot" :class="{ ok: hostReady }" />
      <span>{{ hostReady ? t("hostReady") : t("hostOffline") }}</span>
    </div>

    <div class="header-actions no-drag">
      <Tooltip v-if="showUpdatePill" :text="updatePillTip">
        <button
          class="pill-btn update-pill"
          :class="{
            ready: updateState === 'ready',
            downloading: updateState === 'downloading' || updateState === 'verifying',
          }"
          :style="
            updateState === 'downloading' || updateState === 'verifying'
              ? { '--upd-pct': `${updateStatus?.progress ?? 0}%` }
              : null
          "
          type="button"
          @click="emit('open-update')"
        >
          <span v-if="updateState === 'available'" class="upd-dot" />
          <Icon :name="updateState === 'ready' ? 'CheckCircle' : 'Download'" :size="12" />
          <span>{{ updatePillLabel }}</span>
        </button>
      </Tooltip>

      <Tooltip v-if="attached" :text="t('qhHeaderBtnTip', { key: hooksHotkey || 'Ctrl+Alt+H' })">
        <button class="pill-btn hooks-pill" type="button" @click="emit('open-quick-hooks')">
          <Icon name="Layers" :size="12" />
          <span>{{ t("qhHeaderBtn", { count: hooksCount }) }}</span>
        </button>
      </Tooltip>

      <Tooltip :text="t('modeToggleTip')">
        <button class="pill-btn" :class="{ adv: !isSimple }" type="button" @click="emit('toggle-mode')">
          <span>{{ isSimple ? t("modeSimple") : t("modeAdvanced") }}</span>
        </button>
      </Tooltip>

      <Tooltip :text="t('langToggleTip')">
        <button class="btn text-btn" type="button" @click="emit('toggle-lang')">
          {{ t("langToggleLabel") }}
        </button>
      </Tooltip>

      <Tooltip :text="t('helpBtnTip')">
        <button class="btn text-btn" type="button" @click="emit('open-troubleshooter')">
          <Icon name="HelpCircle" :size="13" />
          <span>{{ t("helpBtn") }}</span>
        </button>
      </Tooltip>

      <span class="vdivider" />

      <Tooltip :text="t('themeTip')">
        <button class="btn" type="button" @click="emit('toggle-theme')">
          <Icon :name="theme === 'dark' ? 'Sun' : 'Moon'" :size="14" />
        </button>
      </Tooltip>
      <Tooltip :text="t('sidebarSideTip')">
        <button class="btn" type="button" @click="emit('toggle-sidebar')">
          <span class="panel-icon" :class="{ flipped: sidebar === 'left' }">
            <Icon name="PanelLeft" :size="14" />
          </span>
        </button>
      </Tooltip>
      <Tooltip :text="t('aboutTip')">
        <button class="btn" type="button" @click="emit('open-about')">
          <Icon name="Info" :size="12" />
        </button>
      </Tooltip>

      <span class="vdivider" />
      <button class="btn wctl" type="button" @click="minimize">
        <Icon name="Minus" :size="14" />
      </button>
      <button class="btn wctl" type="button" @click="toggleMax">
        <Icon name="Square" :size="12" />
      </button>
      <button class="btn wctl close" type="button" @click="close">
        <Icon name="X" :size="14" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  height: 40px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elev);
  padding: 0 12px;
  gap: 14px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}
.brand-mark {
  width: 24px;
  height: 24px;
  border-radius: 5px;
  background: var(--accent);
  color: #fff;
  display: grid;
  place-items: center;
  font-family: var(--sans);
  font-size: 12px;
  font-weight: 700;
}
.ar {
  font-family: var(--app-title);
  font-size: 15px;
}
.en {
  color: var(--fg-faint);
  font-size: 11px;
}
.crumbs {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--fg-muted);
  font-size: var(--fs-small);
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
}
.dot {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: 50%;
  background: #ef4444;
}
.dot.ok {
  background: #22c55e;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.pill-btn {
  height: 24px;
  padding: 0 9px;
  border-radius: 12px;
  border: 1px solid color-mix(in oklab, var(--accent) 55%, var(--border));
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
}
.pill-btn.adv {
  border-color: var(--border);
  background: var(--paper);
  color: var(--fg);
}
.pill-btn.hooks-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.pill-btn.update-pill {
  position: relative;
  overflow: hidden;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  border-color: color-mix(in oklab, var(--accent) 65%, var(--border));
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
}
.pill-btn.update-pill:hover {
  background: color-mix(in oklab, var(--accent) 26%, transparent);
}
.pill-btn.update-pill.ready {
  border-color: color-mix(in oklab, #22c55e 65%, var(--border));
  background: color-mix(in oklab, #22c55e 18%, transparent);
  color: #22c55e;
}
.pill-btn.update-pill.downloading {
  background: linear-gradient(
    90deg,
    color-mix(in oklab, var(--accent) 28%, transparent) var(--upd-pct, 0%),
    color-mix(in oklab, var(--accent) 10%, transparent) var(--upd-pct, 0%)
  );
}
.upd-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 0 color-mix(in oklab, var(--accent) 70%, transparent);
  animation: upd-pulse 2s infinite;
}
@keyframes upd-pulse {
  0% {
    box-shadow: 0 0 0 0 color-mix(in oklab, var(--accent) 65%, transparent);
  }
  70% {
    box-shadow: 0 0 0 5px transparent;
  }
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  min-width: 26px;
  padding: 0 7px;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg);
  font: inherit;
  font-size: var(--fs-small);
  cursor: pointer;
}
.btn.text-btn {
  font-size: 11.5px;
  font-weight: 600;
}
.btn:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
.btn.wctl {
  padding: 0;
  width: 28px;
  justify-content: center;
}
.btn.wctl.close:hover {
  background: #e81123;
  color: #fff;
}
.vdivider {
  width: 1px;
  height: 16px;
  background: var(--border);
  margin: 0 3px;
}
.panel-icon {
  display: inline-flex;
  transition: transform 0.15s;
}
.panel-icon.flipped {
  transform: scaleX(-1);
}
</style>
