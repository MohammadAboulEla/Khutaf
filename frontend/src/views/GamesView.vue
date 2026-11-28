<script setup>
import { onMounted, ref, shallowRef } from "vue";
import Icon from "../components/Icon.vue";
import Tooltip from "../components/Tooltip.vue";
import GameOverridesModal from "../components/GameOverridesModal.vue";
import ClearCacheModal from "../components/ClearCacheModal.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";
import { formatHookLabel } from "../utils/hookLabels.js";

const emit = defineEmits(["open-hooks", "edit-rules"]);
const { t, lang } = useI18n();
const overrideGame = shallowRef(null);

const {
  savedGames,
  cacheCount,
  refreshSavedGames,
  quickAttach,
  detach,
  deleteSavedGame,
  clearSavedGameHooks,
  removeSavedGameHook,
  saveGamePipeline,
  setGameAutoAttach,
  clearScopeCache,
  exportGameProfile,
  importGameProfile,
} = useSession();

const cacheCleared = shallowRef(false);
const clearCacheModalOpen = shallowRef(false);
const clearCacheInitialGame = shallowRef("");
const expandedHooks = ref({});
const profileFeedback = ref({});

function setProfileFeedback(exePath, msg) {
  profileFeedback.value = { ...profileFeedback.value, [exePath]: msg };
  setTimeout(() => {
    const next = { ...profileFeedback.value };
    delete next[exePath];
    profileFeedback.value = next;
  }, 3500);
}

async function onExportProfile(g) {
  try {
    const dst = await exportGameProfile(g.exePath);
    if (dst) {
      setProfileFeedback(g.exePath, t("gvExportedOk"));
    }
  } catch {
    // Error banner handles failure.
  }
}

async function onImportProfile(g) {
  try {
    const res = await importGameProfile(g.exePath);
    if (res && (res.title || res.hooksAdded || res.mutedAdded || res.glossaryAdded || res.rulesAdded || res.embedOverrideApplied)) {
      setProfileFeedback(
        g.exePath,
        t("gvImportedOk", {
          hooks: res.hooksAdded || 0,
          glossary: res.glossaryAdded || 0,
          rules: res.rulesAdded || 0,
        }),
      );
    }
  } catch {
    // Error banner handles failure.
  }
}

function toggleHooks(exePath) {
  expandedHooks.value[exePath] = !expandedHooks.value[exePath];
}

onMounted(() => {
  refreshSavedGames();
});

async function onQuickAttach(exePath) {
  const ok = await quickAttach(exePath);
  if (ok) emit("open-hooks");
}

async function onDetach() {
  await detach();
  await refreshSavedGames();
}

function openClearCache(exePath = "") {
  clearCacheInitialGame.value = exePath;
  clearCacheModalOpen.value = true;
}

async function onConfirmClearCache({ gamePath, includeManual }) {
  await clearScopeCache(gamePath, includeManual);
  clearCacheModalOpen.value = false;
  cacheCleared.value = true;
  setTimeout(() => {
    cacheCleared.value = false;
  }, 1500);
}

function hookTooltip(h) {
  return `${h.hookName || h.hookCode} · ctx ${h.ctx.toString(16)}`;
}

function ruleCount(g) {
  return (g.pipeline?.preRules?.length || 0) + (g.pipeline?.glossary?.length || 0) + (g.pipeline?.postRules?.length || 0);
}
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h2 class="h2">{{ t("gvTitle") }}</h2>
        <p class="sub">{{ t("gvSub") }}</p>
      </div>
      <div class="actions">
        <button class="btn" type="button" @click="refreshSavedGames">
          <Icon name="RefreshCw" :size="13" />
          <span>{{ t("qpRefresh") }}</span>
        </button>
        <button class="btn" type="button" :disabled="cacheCount === 0 && !cacheCleared" @click="openClearCache('')">
          <Icon name="Trash" :size="13" />
          <span>{{ cacheCleared ? t("gvCacheCleared") : t("gvClearCache", { count: cacheCount }) }}</span>
        </button>
      </div>
    </div>

    <div v-if="!savedGames.length" class="empty-card">
      <p>{{ t("gvEmpty") }}</p>
      <button class="btn primary" type="button" @click="emit('open-hooks')">{{ t("gvOpenHooks") }}</button>
    </div>

    <div v-else class="grid">
      <div v-for="g in savedGames" :key="g.exePath" class="card">
        <div class="card-head">
          <div class="title-wrap">
            <div class="title-row">
              <span class="title">{{ g.title }}</span>
              <span v-if="g.attached" class="badge ok">Attached</span>
              <span v-else-if="g.runningPid" class="badge run">Running · PID {{ g.runningPid }}</span>
              <span v-else class="badge">Offline</span>
            </div>
            <Tooltip :text="g.exePath" mono>
              <div class="path">{{ g.exePath }}</div>
            </Tooltip>
          </div>

          <div class="card-actions">
            <button
              v-if="g.attached"
              class="btn danger"
              type="button"
              @click="onDetach"
            >
              Detach
            </button>
            <Tooltip v-else :text="g.runningPid ? 'Attach immediately' : 'Start the game first'">
              <button
                class="btn primary"
                type="button"
                :disabled="!g.runningPid"
                @click="onQuickAttach(g.exePath)"
              >
                Quick Attach
              </button>
            </Tooltip>
            <Tooltip text="Remove game profile">
              <button
                class="btn icon"
                type="button"
                @click="deleteSavedGame(g.exePath)"
              >
                <Icon name="Trash" :size="13" />
              </button>
            </Tooltip>
          </div>
        </div>

        <div class="hooks-box">
          <div class="hooks-head" @click="toggleHooks(g.exePath)">
            <span class="hlbl toggle-lbl">
              <Icon :name="expandedHooks[g.exePath] ? 'ChevronDown' : 'Chevron'" :size="12" />
              <span>Saved Embed Hooks ({{ g.embedHooks?.length || 0 }})</span>
              <span v-if="g.mutedHooks?.length" class="ov-pill">{{ t("gvMutedCount", { count: g.mutedHooks.length }) }}</span>
            </span>
            <button
              v-if="g.embedHooks?.length"
              class="link-btn"
              type="button"
              @click.stop="clearSavedGameHooks(g.exePath)"
            >
              Clear Hooks
            </button>
          </div>
          <template v-if="expandedHooks[g.exePath]">
            <div v-if="!g.embedHooks?.length" class="no-hooks">No saved embed hooks.</div>
            <div v-else class="hook-list">
              <div
                v-for="(h, idx) in g.embedHooks"
                :key="`${h.hookCode}:${h.ctx}:${h.ctx2}:${idx}`"
                class="hook-item"
              >
                <Tooltip :text="hookTooltip(h)" mono class="hname-tip">
                  <span class="hname">{{ formatHookLabel(h, idx, lang) }}</span>
                </Tooltip>
                <span class="hctx">ctx {{ h.ctx.toString(16) }}</span>
                <Tooltip text="Remove hook">
                  <button
                    class="del-hook"
                    type="button"
                    @click="removeSavedGameHook(g.exePath, h)"
                  >
                    ×
                  </button>
                </Tooltip>
              </div>
            </div>
          </template>
        </div>

        <div class="rules-bar">
          <span class="hlbl">
            Custom Rules:
            <template v-if="ruleCount(g)">
              {{ g.pipeline?.preRules?.length || 0 }} pre · {{ g.pipeline?.glossary?.length || 0 }} glossary · {{ g.pipeline?.postRules?.length || 0 }} post
            </template>
            <template v-else>None</template>
          </span>
          <div class="rules-actions">
            <button
              v-if="ruleCount(g)"
              class="link-btn"
              type="button"
              @click="saveGamePipeline(g.exePath, { preRules: [], postRules: [], glossary: [] })"
            >
              Clear Rules
            </button>
            <button
              class="link-btn accent"
              type="button"
              @click="emit('edit-rules', g.exePath)"
            >
              {{ ruleCount(g) ? "Edit Rules" : "+ Add Rules" }}
            </button>
          </div>
        </div>

        <div class="rules-bar">
          <span class="hlbl ov-badges">
            <span>{{ t("gvOverridesLbl") }}</span>
            <span v-if="!g.embedOverride && !g.translatorOverride">{{ t("gvOverridesGlobal") }}</span>
            <span v-if="g.embedOverride" class="ov-pill">{{ t("gvOverridesEmbed") }}</span>
            <span v-if="g.translatorOverride" class="ov-pill">{{ t("gvOverridesTranslator") }}</span>
          </span>
          <button class="link-btn accent" type="button" @click="overrideGame = g">
            {{ t("gvEditOverrides") }}
          </button>
        </div>

        <div class="rules-bar">
          <Tooltip :text="t('gvAutoAttachTip')">
            <label class="auto-chk">
              <input
                type="checkbox"
                :checked="g.autoAttach"
                @change="setGameAutoAttach(g.exePath, $event.target.checked)"
              />
              <span>{{ t("gvAutoAttach") }}</span>
            </label>
          </Tooltip>
          <div class="rules-actions">
            <button class="link-btn" type="button" @click="onExportProfile(g)">
              {{ t("gvExportProfile") }}
            </button>
            <button class="link-btn accent" type="button" @click="onImportProfile(g)">
              {{ t("gvImportProfile") }}
            </button>
            <button
              class="link-btn"
              type="button"
              :disabled="cacheCount === 0"
              @click="openClearCache(g.exePath)"
            >
              {{ t("gvClearGameCache") }}
            </button>
          </div>
        </div>
        <div v-if="profileFeedback[g.exePath]" class="profile-ok">{{ profileFeedback[g.exePath] }}</div>
      </div>
    </div>

    <GameOverridesModal
      :open="Boolean(overrideGame)"
      :game="overrideGame"
      @close="overrideGame = null"
    />
    <ClearCacheModal
      :open="clearCacheModalOpen"
      :games="savedGames"
      :initial-game-path="clearCacheInitialGame"
      :total-count="cacheCount"
      @close="clearCacheModalOpen = false"
      @confirm="onConfirmClearCache"
    />
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
  overflow: auto;
}
.toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 12px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.h2 {
  margin: 0 0 4px;
  font-size: 14px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.actions {
  display: flex;
  gap: 8px;
}
.empty-card {
  padding: 28px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  text-align: center;
  color: var(--fg-muted);
  font-size: 13px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
}
.empty-card p {
  margin: 0;
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(440px, 1fr));
  gap: 12px;
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
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}
.title-wrap {
  min-width: 0;
  flex: 1;
}
.title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.title {
  font-size: 14px;
  font-weight: 600;
  color: var(--ink);
}
.path {
  margin-top: 3px;
  font-family: var(--mono);
  font-size: 11px;
  color: var(--fg-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.badge {
  padding: 2px 7px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-family: var(--mono);
  font-size: 11px;
  color: var(--fg-muted);
}
.badge.ok {
  background: color-mix(in oklab, #16a34a 22%, transparent);
  color: #4ade80;
}
.badge.run {
  background: color-mix(in oklab, var(--accent) 20%, transparent);
  color: var(--accent);
}
.card-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
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
}
.btn.icon {
  padding: 0 8px;
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
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
.hooks-box {
  padding: 8px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.hooks-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
}
.toggle-lbl {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
.hlbl {
  font-size: 11px;
  font-weight: 600;
  color: var(--fg-muted);
}
.link-btn {
  background: none;
  border: none;
  padding: 0;
  color: var(--fg-muted);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.link-btn:hover:not(:disabled) {
  color: var(--accent);
  text-decoration: underline;
}
.link-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.no-hooks {
  font-size: 12px;
  color: var(--fg-muted);
}
.hook-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.hook-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11.5px;
  padding: 3px 6px;
  border-radius: 4px;
  background: var(--bg-elev);
}
.hname-tip {
  flex: 1;
  min-width: 0;
}
.hname {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--mono);
}
.hctx {
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--fg-muted);
  flex: none;
}
.del-hook {
  background: none;
  border: none;
  color: var(--fg-muted);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
  padding: 0 3px;
}
.del-hook:hover {
  color: #f87171;
}
.rules-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.rules-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}
.link-btn.accent {
  color: var(--accent);
  font-weight: 500;
}
.ov-badges {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.ov-pill {
  padding: 1px 6px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
  font-size: 10.5px;
}
.auto-chk {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--fg);
  cursor: pointer;
}
.auto-chk input {
  accent-color: var(--accent);
}
.profile-ok {
  font-size: 11px;
  color: #4ade80;
  padding: 2px 4px;
}
</style>
