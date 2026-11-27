<script setup>
import { computed, onBeforeUnmount, onMounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { usePinnedHookSort } from "../composables/usePinnedHookSort.js";
import { formatHookLabel } from "../utils/hookLabels.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  standalone: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);
const { t, lang } = useI18n();
const {
  hooks,
  hookOutputs,
  savedGames,
  processes,
  status,
  config,
  setEmbed,
  refreshHooks,
  refreshSavedGames,
  refreshProcesses,
  removeSavedGameHook,
  attach,
  quickAttach,
  detach,
  showMainFromHooksPopup,
  saveConfig,
} = useSession();
const { snapshotPinned, sortPinned, needsSort } = usePinnedHookSort(() => hooks.value);

const normPath = (p) => (p || "").toLowerCase().replace(/\//g, "\\");

const search = shallowRef("");
const showAllActive = shallowRef(false);
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
      snapshotPinned();
    }
  },
  { immediate: true },
);

async function toggleVerboseHooks(val) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: { ...config.value.embed, verboseHooks: Boolean(val) },
  });
}

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
    snapshotPinned();
  } finally {
    attachingPid.value = 0;
  }
}

async function onDetach() {
  await detach();
  await Promise.all([refreshProcesses(), refreshSavedGames()]);
}

function onKeydown(e) {
  if (props.open && e.key === "Escape") {
    e.preventDefault();
    emit("close");
  }
}

onMounted(() => window.addEventListener("keydown", onKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));

const savedOnlyHooks = computed(() => {
  if (!status.value.attached || !status.value.gamePath) return [];
  const curPath = normPath(status.value.gamePath);
  const game = savedGames.value.find((g) => normPath(g.exePath) === curPath);
  if (!game?.embedHooks?.length) return [];

  const liveIds = new Set(hooks.value.map((h) => `${h.hookCode}|${h.ctx || 0}|${h.ctx2 || 0}`));
  return game.embedHooks
    .filter((s) => !liveIds.has(`${s.hookCode}|${s.ctx || 0}|${s.ctx2 || 0}`))
    .map((s) => ({
      key: `saved:${s.hookCode}:${(s.ctx || 0).toString(16)}:${(s.ctx2 || 0).toString(16)}`,
      hookCode: s.hookCode,
      hookName: s.hookName || s.hookCode,
      ctx: s.ctx || 0,
      ctx2: s.ctx2 || 0,
      embeddable: true,
      embedding: true,
      savedOnly: true,
      sample: s.sample || "",
    }));
});

const embeddableHooks = computed(() => {
  const q = search.value.trim().toLowerCase();
  const live = hooks.value.filter((h) => h.embeddable);
  const list = showAllActive.value ? [...live, ...savedOnlyHooks.value] : live;
  const filtered = q
    ? list.filter((h) => {
        const text = (hookOutputs.value[h.key] || h.sample || "").toLowerCase();
        const name = (h.hookName || "").toLowerCase();
        const code = (h.hookCode || "").toLowerCase();
        const ctxHex = h.ctx ? h.ctx.toString(16).toLowerCase() : "";
        return text.includes(q) || name.includes(q) || code.includes(q) || ctxHex.includes(q);
      })
    : list;

  return sortPinned(filtered);
});

const activeCount = computed(() => {
  const liveActive = hooks.value.filter((h) => h.embeddable && h.embedding).length;
  return showAllActive.value ? liveActive + savedOnlyHooks.value.length : liveActive;
});

async function onToggleHook(h, checked) {
  if (h.savedOnly) {
    if (!checked && status.value.gamePath) {
      await removeSavedGameHook(status.value.gamePath, h);
    }
    return;
  }
  await setEmbed(h.key, checked);
  await refreshSavedGames();
}

function hookDisplayText(h) {
  if (h.savedOnly) {
    if (h.sample) return h.sample;
    const base = formatHookLabel(h, -1, lang.value);
    return h.ctx ? `${base} · 0x${h.ctx.toString(16)}` : base;
  }
  return hookOutputs.value[h.key] || "—";
}

function hookSubLabel(h) {
  const base = formatHookLabel(h, -1, lang.value);
  return h.ctx ? `${base} · 0x${h.ctx.toString(16)}` : base;
}
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
              <button
                v-else-if="status.attached"
                class="detach-pill"
                type="button"
                :title="t('qhDetachTip')"
                @click="onDetach"
              >
                <Icon name="Stop" :size="11" />
                <span>{{ t("qpDetach") }}</span>
              </button>
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

          <div class="controls">
            <div class="action-bar">
              <div v-if="status.attached" class="filter-pills">
                <label
                  class="toggle-pill"
                  :class="{ on: Boolean(config?.embed?.verboseHooks) }"
                  :title="t('verboseHooksTip')"
                >
                  <input
                    type="checkbox"
                    class="sr-only"
                    :checked="Boolean(config?.embed?.verboseHooks)"
                    @change="toggleVerboseHooks($event.target.checked)"
                  />
                  <span class="pill-dot" />
                  <span>{{ t("verboseHooksLabel") }}</span>
                </label>

                <label
                  class="toggle-pill"
                  :class="{ on: showAllActive }"
                  :title="t('qhShowAllActiveTip')"
                >
                  <input v-model="showAllActive" type="checkbox" class="sr-only" />
                  <span class="pill-dot" />
                  <span>{{ t("qhShowAllActive") }}</span>
                  <span v-if="savedOnlyHooks.length" class="pill-count">+{{ savedOnlyHooks.length }}</span>
                </label>
              </div>

              <div class="bar-actions">
                <button
                  v-if="status.attached"
                  class="bar-icon-btn"
                  :class="{ dirty: needsSort }"
                  type="button"
                  :title="t('sortCheckedTopTip')"
                  :aria-label="t('sortCheckedTop')"
                  :disabled="!embeddableHooks.length"
                  @click="snapshotPinned"
                >
                  <Icon name="ArrowUpDown" :size="13" />
                </button>
                <button
                  class="bar-icon-btn"
                  type="button"
                  :title="t('qhRefreshTip')"
                  :aria-label="t('qhRefreshTip')"
                  :disabled="refreshing"
                  @click="onRefresh"
                >
                  <Icon name="RefreshCw" :size="13" :class="{ spin: refreshing }" />
                </button>
                <button
                  v-if="standalone"
                  class="bar-icon-btn"
                  type="button"
                  :title="t('qhShowAppTip')"
                  :aria-label="t('qhShowAppTip')"
                  @click="showMainFromHooksPopup"
                >
                  <Icon name="Maximize" :size="13" />
                </button>
              </div>
            </div>

            <div v-if="status.attached" class="search-row">
              <div class="search-wrap">
                <Icon name="Search" :size="13" class="search-ico" />
                <input
                  v-model="search"
                  class="search-input"
                  type="text"
                  :placeholder="t('qhSearchPlaceholder')"
                />
                <button
                  v-if="search"
                  class="clear-btn"
                  type="button"
                  :aria-label="t('qhClose')"
                  @click="search = ''"
                >
                  <Icon name="X" :size="11" />
                </button>
              </div>
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

            <div v-else-if="!embeddableHooks.length" class="empty-state">
              <Icon name="Activity" :size="24" class="empty-ico" />
              <div class="empty-msg">{{ t("qhEmpty") }}</div>
              <button
                v-if="!showAllActive && savedOnlyHooks.length"
                class="btn secondary"
                type="button"
                @click="showAllActive = true"
              >
                {{ t("qhRevealSaved", { count: savedOnlyHooks.length }) }}
              </button>
            </div>

            <div v-else class="hooks-list">
              <label
                v-for="h in embeddableHooks"
                :key="h.key"
                class="hook-row"
                :class="{ active: h.embedding, saved: h.savedOnly }"
              >
                <input
                  type="checkbox"
                  class="sr-only"
                  :checked="h.embedding"
                  @change="onToggleHook(h, $event.target.checked)"
                />
                <span class="custom-check" :class="{ checked: h.embedding }">
                  <Icon v-if="h.embedding" name="Check" :size="11" />
                </span>
                <div class="hook-info">
                  <div class="hook-primary">
                    <div
                      class="hook-sample"
                      :class="{ empty: !h.savedOnly && !hookOutputs[h.key] }"
                      dir="auto"
                    >
                      {{ hookDisplayText(h) }}
                    </div>
                    <span v-if="h.savedOnly" class="badge saved">{{ t("qhSavedBadge") }}</span>
                    <span v-else-if="!h.embedding" class="badge new">{{ t("qhNewBadge") }}</span>
                    <span v-else class="badge on">{{ t("qhActiveBadge") }}</span>
                  </div>
                  <div class="hook-meta">{{ hookSubLabel(h) }}</div>
                </div>
              </label>
            </div>
          </div>

          <footer class="modal-foot">
            <div class="hint-wrap">
              <kbd class="kbd">{{ config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }}</kbd>
              <span class="hint-text">{{ t("qhHotkeyHint", { key: config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }) }}</span>
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
  max-width: 620px;
  max-height: 82vh;
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
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
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
.attach-pill:hover:not(:disabled) {
  filter: brightness(1.08);
}
.attach-pill:disabled {
  opacity: 0.6;
  cursor: default;
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
  transition: background 0.14s, border-color 0.14s, color 0.14s;
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
.modal-icon-btn:disabled {
  opacity: 0.5;
  cursor: default;
}
.spin {
  animation: spin 0.6s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.controls {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 9px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.search-row {
  display: flex;
  gap: 7px;
  align-items: center;
}
.search-wrap {
  flex: 1;
  min-width: 0;
  height: 30px;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--bg-elev);
}
.search-wrap:focus-within {
  border-color: color-mix(in srgb, var(--accent) 65%, var(--border));
}
.search-ico {
  color: var(--fg-muted);
  flex: none;
}
.search-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.clear-btn {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--fg-muted);
  cursor: pointer;
}
.clear-btn:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg);
}
.action-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}
.filter-pills {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.bar-actions {
  display: flex;
  align-items: center;
  gap: 3px;
  margin-inline-start: auto;
  flex: none;
}
.bar-icon-btn {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elev);
  color: var(--fg-muted);
  cursor: pointer;
}
.bar-icon-btn:hover:not(:disabled) {
  background: color-mix(in oklab, var(--fg) 10%, var(--bg-elev));
  color: var(--fg);
}
.bar-icon-btn.dirty {
  border-color: var(--accent);
  color: var(--accent);
}
.bar-icon-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.toggle-pill {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 9px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg-muted);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  user-select: none;
  transition: border-color 0.15s, background 0.15s, color 0.15s;
}
.toggle-pill:hover {
  color: var(--fg);
}
.toggle-pill.on {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 12%, var(--paper));
  color: var(--fg);
}
.pill-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--fg-muted);
  opacity: 0.45;
}
.toggle-pill.on .pill-dot {
  background: var(--accent);
  opacity: 1;
}
.pill-count {
  padding: 0 5px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-size: 10px;
  font-weight: 600;
}

.modal-body {
  flex: 1;
  min-height: 160px;
  overflow: auto;
  padding: 10px 14px;
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
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 3px;
  font-size: 11px;
  color: var(--fg-muted);
}
.mini-badge {
  padding: 1px 6px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 8%, transparent);
  font-size: 10px;
  font-weight: 600;
}
.mini-badge.saved {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  color: var(--accent);
}
.attach-btn {
  flex: none;
  height: 28px;
  padding: 0 11px;
  border-color: var(--accent);
  background: var(--accent);
  color: #fff;
  font-size: 11.5px;
}
.attach-btn:hover:not(:disabled) {
  filter: brightness(1.08);
}

.empty-state {
  min-height: 180px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  text-align: center;
  padding: 20px 14px;
}
.empty-ico {
  color: var(--fg-muted);
  opacity: 0.45;
}
.empty-msg {
  color: var(--fg-muted);
  font-size: 12px;
  line-height: 1.5;
  max-width: 300px;
}

.hooks-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.hook-row {
  position: relative;
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-inline-start: 3px solid transparent;
  border-radius: 8px;
  background: var(--bg);
  cursor: pointer;
  transition: border-color 0.12s, background 0.12s;
}
.hook-row:hover {
  border-color: color-mix(in oklab, var(--fg) 20%, var(--border));
}
.hook-row.active {
  border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  border-inline-start-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 6%, var(--bg));
}
.custom-check {
  flex: none;
  margin-top: 2px;
  width: 16px;
  height: 16px;
  border-radius: 4px;
  border: 1.5px solid color-mix(in oklab, var(--fg) 28%, var(--border));
  background: var(--bg-elev);
  display: grid;
  place-items: center;
  color: #fff;
}
.custom-check.checked {
  border-color: var(--accent);
  background: var(--accent);
}
.hook-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.hook-primary {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}
.hook-sample {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  color: var(--fg);
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  max-height: 58px;
}
.hook-sample.empty {
  color: var(--fg-muted);
  font-weight: 400;
  font-style: italic;
}
.hook-meta {
  font-size: 10.5px;
  color: var(--fg-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.badge {
  flex: none;
  font-size: 10px;
  padding: 2px 7px;
  border-radius: 999px;
  font-weight: 600;
}
.badge.new {
  background: rgba(245, 158, 11, 0.16);
  color: #f59e0b;
}
.badge.on {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
  color: var(--accent);
}
.badge.saved {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg-muted);
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
  gap: 6px;
  min-width: 0;
}
.kbd {
  flex: none;
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg);
  font-family: ui-monospace, monospace;
  font-size: 10.5px;
}
.hint-text {
  font-size: 10.5px;
  color: var(--fg-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
}
.btn.secondary {
  background: var(--bg-elev);
}
.btn:hover:not(:disabled) {
  border-color: color-mix(in oklab, var(--fg) 25%, var(--border));
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.14s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
