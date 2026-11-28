<script setup>
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import Tooltip from "./Tooltip.vue";
import HookBrowser from "./hook/HookBrowser.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  standalone: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);
const { t } = useI18n();
const {
  hooks,
  savedGames,
  processes,
  status,
  config,
  refreshHooks,
  refreshSavedGames,
  refreshProcesses,
  attach,
  quickAttach,
  detach,
  togglePause,
  adjustFontScale,
  saveConfig,
  showMainFromHooksPopup,
} = useSession();

const normPath = (p) => (p || "").toLowerCase().replace(/\//g, "\\");

const browserRef = ref(null);
const refreshing = shallowRef(false);
const attachingPid = shallowRef(0);

async function syncAll() {
  await Promise.all([refreshHooks(), refreshSavedGames(), refreshProcesses()]);
}

watch(
  () => props.open,
  async (isOpen) => {
    if (isOpen) {
      await syncAll();
      browserRef.value?.snapshotPinned?.();
      browserRef.value?.focusSearch?.();
    }
  },
  { immediate: true },
);

async function onRefresh() {
  if (refreshing.value) return;
  refreshing.value = true;
  try {
    await syncAll();
  } finally {
    setTimeout(() => {
      refreshing.value = false;
    }, 250);
  }
}

const savedPathSet = computed(() => {
  const s = new Set();
  for (const g of savedGames.value) {
    if (g.exePath) s.add(normPath(g.exePath));
  }
  return s;
});

const runningSupportedGames = computed(() => {
  const list = processes.value.filter((p) => p.supported);
  return [...list].sort((a, b) => {
    const as = savedPathSet.value.has(normPath(a.exePath)) ? 0 : 1;
    const bs = savedPathSet.value.has(normPath(b.exePath)) ? 0 : 1;
    if (as !== bs) return as - bs;
    return (a.title || a.name).localeCompare(b.title || b.name);
  });
});

const activeCount = computed(() =>
  hooks.value.filter((h) => h.embeddable && h.embedding && !h.muted).length,
);

const activeDisplayMode = computed(() => config.value?.embed?.displayMode ?? 0);
const fontScaleLabel = computed(() => `${Number(config.value?.embed?.fontSizeRelative || 1).toFixed(1)}x`);

async function setDisplayMode(mode) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: { ...config.value.embed, displayMode: mode },
  });
}

async function onQuickAttach(proc) {
  const target = proc || runningSupportedGames.value[0];
  if (!target || attachingPid.value) return;
  attachingPid.value = target.pid;
  try {
    if (savedPathSet.value.has(normPath(target.exePath))) {
      await quickAttach(target.exePath);
    } else {
      await attach(target.pid);
    }
    await syncAll();
    browserRef.value?.snapshotPinned?.();
  } finally {
    attachingPid.value = 0;
  }
}

async function onDetach() {
  await detach();
  await Promise.all([refreshProcesses(), refreshSavedGames()]);
}

function onKeydown(e) {
  if (props.open && e.key === "Escape" && !e.defaultPrevented) {
    e.preventDefault();
    emit("close");
  }
}

onMounted(() => window.addEventListener("keydown", onKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));
</script>

<template>
  <Teleport :disabled="standalone" to="body">
    <Transition name="fade">
      <div
        v-if="open"
        :class="standalone ? 'standalone-host' : 'modal-overlay'"
        @click.self="!standalone && emit('close')"
      >
        <div
          class="modal-box"
          :class="{ standalone }"
          role="dialog"
          aria-modal="true"
          :aria-label="t('qhTitle')"
        >
          <header class="modal-head" :class="{ drag: standalone }">
            <div class="title-col">
              <div class="title-row">
                <span class="status-dot" :class="{ live: status.attached }" />
                <span class="modal-title">{{ t("qhTitle") }}</span>
              </div>
              <div class="modal-sub">
                {{
                  status.attached
                    ? t("qhSubAttached", { game: status.gameName, active: activeCount })
                    : t("qhSubIdle")
                }}
              </div>
            </div>

            <div class="head-actions" :class="{ 'no-drag': standalone }">
              <button
                v-if="!status.attached && runningSupportedGames.length"
                class="attach-pill"
                type="button"
                :disabled="Boolean(attachingPid)"
                @click="onQuickAttach()"
              >
                <Icon name="Zap" :size="12" />
                <span>{{ attachingPid ? t("qhAttaching") : t("qhQuickAttach") }}</span>
              </button>
              <Tooltip v-else-if="status.attached" :text="t('qhDetachTip')" placement="bottom">
                <button
                  class="detach-pill"
                  type="button"
                  @click="onDetach"
                >
                  <Icon name="Stop" :size="11" />
                  <span>{{ t("qpDetach") }}</span>
                </button>
              </Tooltip>
              <Tooltip :text="t('qhRefreshTip')" placement="bottom">
                <button
                  class="modal-icon-btn"
                  type="button"
                  :aria-label="t('qhRefreshTip')"
                  :disabled="refreshing"
                  @click="onRefresh"
                >
                  <Icon name="RefreshCw" :size="13" :class="{ spin: refreshing }" />
                </button>
              </Tooltip>
              <Tooltip v-if="standalone" :text="t('qhShowAppTip')" placement="bottom">
                <button
                  class="modal-icon-btn"
                  type="button"
                  :aria-label="t('qhShowAppTip')"
                  @click="showMainFromHooksPopup"
                >
                  <Icon name="Maximize" :size="13" />
                </button>
              </Tooltip>
              <button
                class="modal-icon-btn"
                type="button"
                :aria-label="t('qhClose')"
                @click="emit('close')"
              >
                <Icon name="X" :size="14" />
              </button>
            </div>
          </header>

          <!-- Compact In-Game Live Session Strip -->
          <div v-if="status.attached" class="session-strip">
            <div class="seg">
              <button
                type="button"
                class="seg-btn"
                :class="{ active: activeDisplayMode === 0 }"
                @click="setDisplayMode(0)"
              >
                {{ t("qhModeAr") }}
              </button>
              <button
                type="button"
                class="seg-btn"
                :class="{ active: activeDisplayMode === 1 }"
                @click="setDisplayMode(1)"
              >
                {{ t("qhModeBoth") }}
              </button>
              <button
                type="button"
                class="seg-btn"
                :class="{ active: activeDisplayMode === 3 }"
                @click="setDisplayMode(3)"
              >
                {{ t("qhModeOri") }}
              </button>
            </div>

            <div class="strip-right">
              <button
                class="strip-pill"
                :class="{ warn: status.paused }"
                type="button"
                @click="togglePause"
              >
                <Icon :name="status.paused ? 'Play' : 'Pause'" :size="11" />
                <span>{{ status.paused ? t("qhResume") : t("qhPause") }}</span>
              </button>

              <Tooltip :text="t('qhFontScaleTip')" placement="bottom">
                <div class="font-stepper">
                  <button class="step-btn" type="button" @click="adjustFontScale(-0.1)">A-</button>
                  <span class="step-val">{{ fontScaleLabel }}</span>
                  <button class="step-btn" type="button" @click="adjustFontScale(0.1)">A+</button>
                </div>
              </Tooltip>
            </div>
          </div>

          <div class="modal-body">
            <template v-if="!status.attached">
              <div v-if="runningSupportedGames.length" class="proc-section">
                <div class="sec-label">{{ t("qhRunningGames") }}</div>
                <div class="proc-list">
                  <div v-for="p in runningSupportedGames" :key="p.pid" class="proc-card">
                    <div class="proc-info">
                      <div class="proc-title">{{ p.title || p.name }}</div>
                      <div class="proc-meta">
                        <span>{{ p.name }}</span>
                        <span v-if="p.engine" class="mini-badge">{{ p.engine }}</span>
                        <span v-if="savedPathSet.has(normPath(p.exePath))" class="mini-badge saved">
                          {{ t("qhSavedBadge") }}
                        </span>
                      </div>
                    </div>
                    <button
                      class="btn attach-btn"
                      type="button"
                      :disabled="Boolean(attachingPid)"
                      @click="onQuickAttach(p)"
                    >
                      <Icon name="Zap" :size="12" />
                      <span>
                        {{ attachingPid === p.pid ? t("qhAttaching") : t("qhQuickAttach") }}
                      </span>
                    </button>
                  </div>
                </div>
              </div>
              <div v-else class="empty-state">
                <Icon name="Gamepad" :size="26" class="empty-ico" />
                <div class="empty-msg">{{ t("qhNoRunningGames") }}</div>
                <button class="btn secondary" type="button" :disabled="refreshing" @click="onRefresh">
                  <Icon name="RefreshCw" :size="12" :class="{ spin: refreshing }" />
                  <span>{{ t("qpRefresh") }}</span>
                </button>
              </div>
            </template>

            <HookBrowser
              v-else
              ref="browserRef"
              variant="picker"
              :active="open"
              :auto-focus-search="true"
            />
          </div>

          <footer class="modal-foot">
            <div class="hint-wrap">
              <kbd class="kbd">{{ config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }}</kbd>
              <span class="hint-text">
                {{ status.attached ? t("qhKeyboardHint") : t("qhHotkeyHint", { key: config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }) }}
              </span>
            </div>
            <button class="btn secondary" type="button" @click="emit('close')">{{ t("qhClose") }}</button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.standalone-host {
  width: 100%;
  height: 100%;
  display: flex;
}
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 9200;
  background: rgba(0, 0, 0, 0.62);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}
.modal-box {
  width: 100%;
  max-width: 680px;
  max-height: 84vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.45);
  overflow: hidden;
}
.modal-box.standalone {
  max-width: none;
  max-height: none;
  height: 100%;
  border-radius: 0;
  box-shadow: none;
}
.drag {
  --wails-draggable: drag;
  cursor: move;
}
.no-drag {
  --wails-draggable: no-drag;
  cursor: default;
}
.modal-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 14px;
  background: linear-gradient(180deg, color-mix(in oklab, var(--paper) 85%, var(--bg-elev)), var(--bg-elev));
  border-bottom: 1px solid var(--border);
}
.title-col {
  min-width: 0;
  flex: 1;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 7px;
}
.status-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--fg-muted);
  opacity: 0.5;
  flex: none;
}
.status-dot.live {
  background: #22c55e;
  opacity: 1;
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.65);
}
.modal-title {
  font-size: 13.5px;
  font-weight: 700;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.modal-sub {
  font-size: 11px;
  color: var(--fg-muted);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 3px;
  flex: none;
}
.attach-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  margin-inline-end: 4px;
  border: none;
  border-radius: 999px;
  background: var(--accent);
  color: #fff;
  font: inherit;
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.detach-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  margin-inline-end: 4px;
  border: 1px solid rgba(220, 38, 38, 0.45);
  border-radius: 999px;
  background: rgba(220, 38, 38, 0.14);
  color: #f87171;
  font: inherit;
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.detach-pill:hover {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}
.modal-icon-btn {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  background: transparent;
  border: none;
  border-radius: 6px;
  color: var(--fg-muted);
  cursor: pointer;
}
.modal-icon-btn:hover:not(:disabled) {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg);
}
.spin {
  animation: spin 0.6s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.session-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 6px 12px;
  background: color-mix(in oklab, var(--paper) 65%, var(--bg));
  border-bottom: 1px solid var(--border);
}
.seg {
  display: inline-flex;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 2px;
  gap: 2px;
}
.seg-btn {
  padding: 2px 8px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--fg-muted);
  font: inherit;
  font-size: 10.5px;
  font-weight: 500;
  cursor: pointer;
}
.seg-btn.active {
  background: var(--accent);
  color: #fff;
  font-weight: 600;
}
.strip-right {
  display: flex;
  align-items: center;
  gap: 6px;
}
.strip-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 23px;
  padding: 0 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: 10.5px;
  font-weight: 600;
  cursor: pointer;
}
.strip-pill.warn {
  border-color: rgba(251, 191, 36, 0.55);
  background: rgba(251, 191, 36, 0.14);
  color: #fbbf24;
}
.font-stepper {
  display: inline-flex;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elev);
  overflow: hidden;
}
.step-btn {
  height: 21px;
  padding: 0 6px;
  border: none;
  background: transparent;
  color: var(--fg-muted);
  font: inherit;
  font-size: 10.5px;
  font-weight: 600;
  cursor: pointer;
}
.step-btn:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg);
}
.step-val {
  padding: 0 4px;
  font-family: var(--mono);
  font-size: 10px;
  color: var(--fg);
}

.modal-body {
  flex: 1;
  min-height: 220px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.proc-section {
  padding: 12px 14px;
  overflow: auto;
}
.sec-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-muted);
  margin-bottom: 8px;
}
.proc-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.proc-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg);
}
.proc-info {
  min-width: 0;
  flex: 1;
}
.proc-title {
  font-size: 12.5px;
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.proc-meta {
  display: flex;
  align-items: center;
  gap: 5px;
  flex-wrap: wrap;
  margin-top: 2px;
  font-size: 11px;
  color: var(--fg-muted);
}
.mini-badge {
  padding: 1px 5px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-size: 10px;
}
.mini-badge.saved {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
  color: var(--accent);
}
.empty-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 28px 16px;
  text-align: center;
  color: var(--fg-muted);
}
.empty-ico {
  opacity: 0.55;
}
.empty-msg {
  font-size: 12px;
  max-width: 340px;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 11px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: 11.5px;
  font-weight: 600;
  cursor: pointer;
}
.attach-btn {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.modal-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 14px;
  border-top: 1px solid var(--border);
  background: var(--bg);
}
.hint-wrap {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
  flex: 1;
}
.kbd {
  padding: 2px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-elev);
  font-family: var(--mono);
  font-size: 10px;
  color: var(--fg);
  flex: none;
}
.hint-text {
  font-size: 10.5px;
  color: var(--fg-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
