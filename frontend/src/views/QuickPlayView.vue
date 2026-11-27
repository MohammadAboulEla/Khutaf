<script setup>
import { computed, shallowRef } from "vue";
import Icon from "../components/Icon.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { usePinnedHookSort } from "../composables/usePinnedHookSort.js";
import { formatHookLabel } from "../utils/hookLabels.js";

const emit = defineEmits([
  "open-translators",
  "open-troubleshooter",
  "open-wizard",
]);

const {
  status,
  processes,
  hooks,
  hookOutputs,
  embedFeed,
  savedGames,
  config,
  unityFontStatus,
  refreshProcesses,
  refreshSavedGames,
  attach,
  quickAttach,
  detach,
  setEmbed,
  openFontDownloadPrompt,
  saveConfig,
} = useSession();

const { t, lang } = useI18n();
const { snapshotPinned, sortPinned, needsSort } = usePinnedHookSort(
  () => hooks.value,
);

const showAllProcs = shallowRef(false);
const refreshing = shallowRef(false);

const savedByPath = computed(() => {
  const m = new Map();
  for (const g of savedGames.value) {
    m.set(g.exePath.toLowerCase(), g);
  }
  return m;
});

const visibleProcesses = computed(() => {
  const list = showAllProcs.value
    ? processes.value
    : processes.value.filter((p) => p.supported);
  return list.filter((p) => !isProcessAttached(p));
});

const embeddableHooks = computed(() =>
  sortPinned(hooks.value.filter((h) => h.embeddable)),
);

const activeDisplayMode = computed(() => config.value?.embed?.displayMode ?? 0);

const activeProviderName = computed(
  () => status.value.provider || config.value?.translator?.active || "bing",
);

async function onRefresh() {
  refreshing.value = true;
  try {
    await Promise.all([refreshProcesses(), refreshSavedGames()]);
  } finally {
    setTimeout(() => {
      refreshing.value = false;
    }, 350);
  }
}

async function onConnect(proc) {
  const saved = savedByPath.value.get((proc.path || "").toLowerCase());
  if (saved) {
    await quickAttach(saved.exePath);
  } else {
    await attach(proc.pid);
  }
}

async function setDisplayMode(mode) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: {
      ...config.value.embed,
      displayMode: mode,
    },
  });
}

function isProcessAttached(p) {
  return status.value.attached && (status.value.pids || []).includes(p.pid);
}

function isProcessSaved(p) {
  return savedByPath.value.has((p.path || "").toLowerCase());
}
</script>

<template>
  <div class="page">
    <div class="hero-bar">
      <div>
        <h2 class="h2">{{ t("qpTitle") }}</h2>
        <p class="sub">{{ t("qpSubtitle") }}</p>
        <div class="hotkey-tip hero-tip">
          <Icon name="Sparkles" :size="13" />
          <span>{{ t("qpReassureTip") }}</span>
        </div>
      </div>
      <div class="hero-actions">
        <button class="btn" type="button" @click="emit('open-wizard')">
          <Icon name="Sparkles" :size="13" />
          <span>{{ t("qpWizardBtn") }}</span>
        </button>
        <button
          class="btn warn-btn"
          type="button"
          @click="emit('open-troubleshooter')"
        >
          <Icon name="HelpCircle" :size="13" />
          <span>{{ t("qpTroubleBtn") }}</span>
        </button>
      </div>
    </div>

    <div class="readiness-strip">
      <div class="pill-card" :class="unityFontStatus.installed ? 'ok' : 'warn'">
        <Icon
          :name="unityFontStatus.installed ? 'CheckCircle' : 'AlertTri'"
          :size="15"
        />
        <span class="pill-text">
          {{
            unityFontStatus.installed
              ? t("qpFontsReady", { count: unityFontStatus.count })
              : t("qpFontsMissing")
          }}
        </span>
        <button
          v-if="!unityFontStatus.installed"
          class="btn primary small"
          type="button"
          :disabled="unityFontStatus.downloading"
          @click="openFontDownloadPrompt"
        >
          {{
            unityFontStatus.downloading
              ? t("qpDownloadingFonts", { pct: unityFontStatus.progress })
              : t("qpDownloadFonts")
          }}
        </button>
      </div>

      <div class="pill-card">
        <Icon name="Globe" :size="15" />
        <span class="pill-text">{{
          t("qpTranslatorActive", { name: activeProviderName })
        }}</span>
        <button
          class="btn small"
          type="button"
          @click="emit('open-translators')"
        >
          <Icon name="Zap" :size="12" />
          <span>{{ t("qpCompareTranslators") }}</span>
        </button>
      </div>
    </div>

    <section class="card">
      <div class="card-head">
        <div>
          <h3 class="h3">{{ t("qpStep1Title") }}</h3>
          <p class="sub">{{ t("qpStep1Hint") }}</p>
        </div>
        <div class="card-tools">
          <label class="chk">
            <input v-model="showAllProcs" type="checkbox" />
            <span>{{ t("qpShowAllProcs") }}</span>
          </label>
          <button
            class="btn refresh-btn"
            :class="{
              highlight: !status.attached && !visibleProcesses.length,
              spinning: refreshing,
            }"
            type="button"
            :disabled="refreshing"
            @click="onRefresh"
          >
            <Icon name="RefreshCw" :size="13" />
            <span>{{ t("qpRefresh") }}</span>
          </button>
        </div>
      </div>

      <div v-if="status.attached" class="connected-bar">
        <div class="connected-info">
          <span class="badge ok">{{ t("qpConnected") }}</span>
          <strong>{{ status.gameName }}</strong>
          <span v-if="status.engine" class="badge">{{ status.engine }}</span>
        </div>
        <button class="btn danger" type="button" @click="detach">
          <Icon name="Stop" :size="13" />
          <span>{{ t("qpDetach") }}</span>
        </button>
      </div>

      <div
        v-if="!status.attached && !visibleProcesses.length"
        class="empty-box"
      >
        <Icon name="Gamepad" :size="24" />
        <div class="empty-title">{{ t("qpNoGames") }}</div>
        <div class="sub">{{ t("qpNoGamesHint") }}</div>
      </div>

      <div v-else-if="visibleProcesses.length" class="proc-grid">
        <div
          v-for="p in visibleProcesses"
          :key="p.pid"
          class="proc-card"
          :class="{ unsupported: !p.supported, active: isProcessAttached(p) }"
        >
          <div class="proc-meta">
            <div class="proc-title">{{ p.title || p.name }}</div>
            <div class="proc-sub">
              <span>{{ p.name }}</span>
              <span v-if="p.engine" class="badge">{{ p.engine }}</span>
              <span v-if="isProcessSaved(p)" class="badge saved">{{
                t("qpSavedBadge")
              }}</span>
              <span v-if="!p.is64" class="badge bad">{{
                t("qp32BitBadge")
              }}</span>
              <span v-else-if="!p.supported" class="badge bad">{{
                t("qpUnsupportedBadge")
              }}</span>
            </div>
          </div>

          <button
            v-if="isProcessAttached(p)"
            class="btn danger small"
            type="button"
            @click="detach"
          >
            {{ t("qpDetach") }}
          </button>
          <button
            v-else
            class="btn primary small"
            type="button"
            :disabled="!p.supported"
            @click="onConnect(p)"
          >
            <Icon name="Play" :size="12" />
            <span>{{
              isProcessSaved(p) ? t("qpQuickConnect") : t("qpConnect")
            }}</span>
          </button>
        </div>
      </div>

      <div class="hotkey-tip">
        <Icon name="Keyboard" :size="14" />
        <span>{{
          t("qpHooksHotkeyTip", {
            key: config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H",
          })
        }}</span>
      </div>

      <p class="notice">{{ t("qpOfflineNotice") }}</p>
    </section>

    <section v-if="status.attached" class="card">
      <div class="card-head">
        <div>
          <h3 class="h3">{{ t("qpStep2Title") }}</h3>
          <p class="sub">{{ t("qpStep2Hint") }}</p>
        </div>
        <button
          class="btn small sort-btn"
          :class="{ dirty: needsSort }"
          type="button"
          :title="t('sortCheckedTopTip')"
          :disabled="!embeddableHooks.length"
          @click="snapshotPinned"
        >
          <Icon name="ArrowUpDown" :size="12" />
          <span>{{ t("sortCheckedTop") }}</span>
        </button>
      </div>

      <details class="hooks-details">
        <summary>
          {{ t("qpShowHooks", { count: embeddableHooks.length }) }}
        </summary>
        <div v-if="config" class="verbose-row">
          <label class="chk" :title="t('verboseHooksTip')">
            <input
              type="checkbox"
              :checked="Boolean(config?.embed?.verboseHooks)"
              @change="saveConfig({ ...config, embed: { ...config.embed, verboseHooks: $event.target.checked } })"
            />
            <span>{{ t("verboseHooksLabel") }}</span>
          </label>
        </div>
        <div v-if="!embeddableHooks.length" class="sub pad-top">
          {{ t("qpNoHooksYet") }}
        </div>
        <div v-else class="hooks-list">
          <label
            v-for="(h, idx) in embeddableHooks"
            :key="h.key"
            class="hook-row"
            :class="{ active: h.embedding }"
          >
            <input
              type="checkbox"
              :checked="h.embedding"
              @change="setEmbed(h.key, $event.target.checked)"
            />
            <div class="hook-info">
              <div
                class="hook-sample"
                :class="{ empty: !hookOutputs[h.key] }"
                dir="auto"
              >
                {{ hookOutputs[h.key] || "—" }}
              </div>
              <div class="hook-name">{{ formatHookLabel(h, idx, lang) }}</div>
            </div>
          </label>
        </div>
      </details>
    </section>

    <section v-if="status.attached" class="card">
      <div class="card-head">
        <h3 class="h3">{{ t("qpStep3Title") }}</h3>
        <div class="seg">
          <button
            type="button"
            class="seg-btn"
            :class="{ active: activeDisplayMode === 0 }"
            @click="setDisplayMode(0)"
          >
            {{ t("qpDisplayTransOnly") }}
          </button>
          <button
            type="button"
            class="seg-btn"
            :class="{ active: activeDisplayMode === 1 }"
            @click="setDisplayMode(1)"
          >
            {{ t("qpDisplayOriTrans") }}
          </button>
          <button
            type="button"
            class="seg-btn"
            :class="{ active: activeDisplayMode === 3 }"
            @click="setDisplayMode(3)"
          >
            {{ t("qpDisplayOriOnly") }}
          </button>
        </div>
      </div>

      <details class="hooks-details">
        <summary>
          {{ t("qpLiveFeedTitle") }} ({{ embedFeed.length }})
        </summary>
        <div v-if="!embedFeed.length" class="empty-mini">
          {{ t("qpLiveFeedEmpty") }}
        </div>
        <div v-else class="feed-list">
          <div
            v-for="(item, idx) in embedFeed.slice(0, 12)"
            :key="idx"
            class="feed-item"
          >
            <div class="feed-src" dir="ltr">{{ item.original }}</div>
            <div v-if="item.translated" class="feed-ar" dir="rtl">
              {{ item.translated }}
            </div>
            <div v-if="item.error" class="feed-err">{{ item.error }}</div>
          </div>
        </div>
      </details>
    </section>
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  height: 100%;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.hero-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 12px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.hero-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.h2 {
  margin: 0 0 4px;
  font-size: 15px;
  font-weight: 700;
}
.h3 {
  margin: 0 0 3px;
  font-size: 13.5px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.readiness-strip {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 10px;
}
.pill-card {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
}
.pill-card.ok {
  border-color: rgba(74, 222, 128, 0.35);
}
.pill-card.warn {
  border-color: rgba(251, 191, 36, 0.45);
}
.pill-text {
  flex: 1;
  font-weight: 500;
}
.card {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.card-tools {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--fg-muted);
  cursor: pointer;
}
.connected-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 12px;
  background: color-mix(in oklab, #16a34a 14%, var(--paper));
  border: 1px solid rgba(74, 222, 128, 0.35);
  border-radius: 6px;
}
.connected-info {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.empty-box {
  padding: 24px 14px;
  text-align: center;
  background: var(--paper);
  border: 1px dashed var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  color: var(--fg-muted);
}
.empty-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
}
.proc-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
  gap: 8px;
}
.proc-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.proc-card.active {
  border-color: var(--accent);
}
.proc-card.unsupported {
  opacity: 0.6;
}
.proc-meta {
  min-width: 0;
  flex: 1;
}
.proc-title {
  font-weight: 600;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.proc-sub {
  margin-top: 3px;
  display: flex;
  align-items: center;
  gap: 5px;
  flex-wrap: wrap;
  font-size: 11px;
  color: var(--fg-muted);
}
.badge {
  padding: 1px 6px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-size: 10.5px;
}
.badge.ok {
  background: color-mix(in oklab, #16a34a 22%, transparent);
  color: #4ade80;
}
.badge.saved {
  background: color-mix(in oklab, var(--accent) 20%, transparent);
  color: var(--accent);
}
.badge.bad {
  background: color-mix(in oklab, #dc2626 20%, transparent);
  color: #f87171;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  white-space: nowrap;
}
.btn.small {
  height: 26px;
  padding: 0 10px;
  font-size: 11.5px;
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.btn.danger {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}
.btn.warn-btn {
  border-color: rgba(251, 191, 36, 0.45);
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.sort-btn.dirty {
  border-color: var(--accent);
  color: var(--accent);
}
.notice {
  margin: 0;
  font-size: 11px;
  color: var(--fg-muted);
}
.hotkey-tip {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  max-width: 100%;
  align-self: flex-start;
  gap: 8px;
  padding: 7px 10px;
  border-radius: 6px;
  background: color-mix(in oklab, var(--accent) 12%, var(--paper));
  border: 1px solid color-mix(in oklab, var(--accent) 32%, var(--border));
  font-size: 12px;
  color: var(--fg);
}
.hero-tip {
  margin-top: 8px;
}
.hooks-details summary {
  cursor: pointer;
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-muted);
}
.pad-top {
  padding-top: 8px;
}
.verbose-row {
  margin-top: 8px;
}
.hooks-list {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 220px;
  overflow: auto;
}
.hook-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 7px 9px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  cursor: pointer;
}
.hook-row.active {
  border-color: var(--accent);
}
.hook-info {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.hook-sample {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  color: var(--fg);
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.hook-sample.empty {
  color: var(--fg-muted);
  font-weight: 400;
  font-style: italic;
}
.hook-name {
  font-size: 11px;
  font-weight: 500;
  color: var(--fg-muted);
}
.seg {
  display: inline-flex;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 2px;
  gap: 2px;
}
.seg-btn {
  padding: 4px 10px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--fg-muted);
  font: inherit;
  font-size: 11.5px;
  cursor: pointer;
}
.seg-btn.active {
  background: var(--accent);
  color: #fff;
  font-weight: 600;
}
.empty-mini {
  margin-top: 8px;
  padding: 12px;
  text-align: center;
  font-size: 12px;
  color: var(--fg-muted);
  background: var(--paper);
  border-radius: 5px;
}
.feed-list {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 240px;
  overflow: auto;
}
.feed-item {
  padding: 7px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
}
.feed-src {
  font-size: 11.5px;
  color: var(--fg-muted);
}
.feed-ar {
  font-family: var(--arabic);
  font-size: 14px;
  margin-top: 2px;
}
.feed-err {
  font-size: 11px;
  color: #f87171;
  margin-top: 2px;
}
.refresh-btn {
  border-color: color-mix(in oklab, var(--accent) 55%, var(--border));
  background: color-mix(in oklab, var(--accent) 14%, var(--paper));
  color: var(--accent);
  font-weight: 600;
}
.refresh-btn:hover:not(:disabled) {
  background: color-mix(in oklab, var(--accent) 22%, var(--paper));
}
.refresh-btn.highlight {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
  box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 22%, transparent);
}
.refresh-btn.highlight:hover:not(:disabled) {
  opacity: 0.92;
  background: var(--accent);
}
.refresh-btn.spinning svg {
  animation: qp-spin 0.6s linear infinite;
}
@keyframes qp-spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
