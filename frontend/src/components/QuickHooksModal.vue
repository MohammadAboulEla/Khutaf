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
  status,
  config,
  setEmbed,
  refreshHooks,
  refreshSavedGames,
  removeSavedGameHook,
  showMainFromHooksPopup,
  saveConfig,
} = useSession();
const { snapshotPinned, sortPinned, needsSort } = usePinnedHookSort(() => hooks.value);

async function toggleVerboseHooks(val) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: { ...config.value.embed, verboseHooks: Boolean(val) },
  });
}

watch(
  () => props.open,
  async (isOpen) => {
    if (isOpen) {
      await Promise.all([refreshHooks(), refreshSavedGames()]);
      snapshotPinned();
    }
  },
  { immediate: true },
);

const search = shallowRef("");
const showAllActive = shallowRef(false);
const refreshing = shallowRef(false);

async function onRefresh() {
  if (refreshing.value) return;
  refreshing.value = true;
  try {
    await Promise.all([refreshHooks(), refreshSavedGames()]);
  } finally {
    setTimeout(() => {
      refreshing.value = false;
    }, 250);
  }
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
  const norm = (p) => (p || "").toLowerCase().replace(/\//g, "\\");
  const curPath = norm(status.value.gamePath);
  const game = savedGames.value.find((g) => norm(g.exePath) === curPath);
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
              <div class="modal-title">{{ t("qhTitle") }}</div>
              <div class="modal-sub">
                {{ status.attached ? t("qhSubAttached", { game: status.gameName, active: activeCount }) : t("qhSubIdle") }}
              </div>
            </div>
            <div class="head-actions" :class="{ 'no-drag': standalone }">
              <button
                class="modal-icon-btn"
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
                class="modal-icon-btn"
                type="button"
                :title="t('qhShowAppTip')"
                :aria-label="t('qhShowAppTip')"
                @click="showMainFromHooksPopup"
              >
                <Icon name="Maximize" :size="13" />
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

          <div class="toolbar">
            <input
              v-model="search"
              class="search-input"
              type="text"
              :placeholder="t('qhSearchPlaceholder')"
            />
            <button
              class="btn sort-btn"
              :class="{ dirty: needsSort }"
              type="button"
              :title="t('sortCheckedTopTip')"
              :disabled="!embeddableHooks.length"
              @click="snapshotPinned"
            >
              <Icon name="ArrowUpDown" :size="13" />
              <span>{{ t("sortCheckedTop") }}</span>
            </button>
          </div>

          <div class="verbose-bar">
            <label class="chk" :title="t('verboseHooksTip')">
              <input
                type="checkbox"
                :checked="Boolean(config?.embed?.verboseHooks)"
                @change="toggleVerboseHooks($event.target.checked)"
              />
              <span>{{ t("verboseHooksLabel") }}</span>
            </label>
            <label v-if="status.attached" class="chk" :title="t('qhShowAllActiveTip')">
              <input v-model="showAllActive" type="checkbox" />
              <span>
                {{ t("qhShowAllActive") }}<template v-if="savedOnlyHooks.length"> (+{{ savedOnlyHooks.length }})</template>
              </span>
            </label>
          </div>

          <div class="modal-body">
            <div v-if="!status.attached" class="empty-msg">{{ t("qhNotAttached") }}</div>
            <div v-else-if="!embeddableHooks.length" class="empty-msg">{{ t("qhEmpty") }}</div>
            <div v-else class="hooks-list">
              <label
                v-for="h in embeddableHooks"
                :key="h.key"
                class="hook-row"
                :class="{ active: h.embedding }"
              >
                <input
                  type="checkbox"
                  :checked="h.embedding"
                  @change="onToggleHook(h, $event.target.checked)"
                />
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
                </div>
              </label>
            </div>
          </div>

          <footer class="modal-foot">
            <span class="hint-text">{{ t("qhHotkeyHint", { key: config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }) }}</span>
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
  cursor: pointer;
}
.modal-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.modal-title {
  font-size: 14px;
  font-weight: 700;
}
.modal-sub {
  font-size: 11.5px;
  color: var(--fg-muted);
  margin-top: 2px;
}
.head-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.modal-icon-btn {
  display: grid;
  place-items: center;
  width: 26px;
  height: 26px;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg-muted);
  cursor: pointer;
}
.modal-icon-btn:hover {
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
.toolbar {
  display: flex;
  gap: 8px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.verbose-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 14px;
  padding: 6px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--paper);
}
.verbose-bar .chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--fg-muted);
  cursor: pointer;
}
.verbose-bar .chk input[type="checkbox"] {
  accent-color: var(--accent);
}
.search-input {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.modal-body {
  flex: 1;
  min-height: 160px;
  overflow: auto;
  padding: 12px 16px;
}
.empty-msg {
  text-align: center;
  padding: 28px 12px;
  color: var(--fg-muted);
  font-size: 12.5px;
}
.hooks-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.hook-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--bg);
  cursor: pointer;
}
.hook-row.active {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 7%, var(--bg));
}
.hook-row input[type="checkbox"] {
  margin-top: 3px;
  accent-color: var(--accent);
}
.hook-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
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
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.45;
  color: var(--fg);
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  max-height: 64px;
}
.hook-sample.empty {
  color: var(--fg-muted);
  font-weight: 400;
  font-style: italic;
}
.badge {
  flex: none;
  font-size: 10.5px;
  padding: 2px 7px;
  border-radius: 4px;
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
  background: color-mix(in srgb, var(--fg) 10%, transparent);
  color: var(--fg-muted);
}
.modal-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
}
.hint-text {
  font-size: 11px;
  color: var(--fg-muted);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--fg);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.sort-btn.dirty {
  border-color: var(--accent);
  color: var(--accent);
}
</style>
