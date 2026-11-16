<script setup>
import { onMounted, shallowRef } from "vue";
import Icon from "../components/Icon.vue";
import Tooltip from "../components/Tooltip.vue";
import { useSession } from "../composables/useSession.js";

const emit = defineEmits(["open-hooks"]);

const {
  savedGames,
  cacheCount,
  refreshSavedGames,
  quickAttach,
  detach,
  deleteSavedGame,
  clearSavedGameHooks,
  removeSavedGameHook,
  clearTranslationCache,
} = useSession();

const cacheCleared = shallowRef(false);

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

async function onClearCache() {
  await clearTranslationCache();
  cacheCleared.value = true;
  setTimeout(() => {
    cacheCleared.value = false;
  }, 1500);
}

function formatHookLabel(h, idx) {
  const src = `${h.hookCode || ""} ${h.hookName || ""}`.toLowerCase();
  let base = h.hookName || h.hookCode || "Text Hook";
  if (src.includes("textmeshprougui")) base = "Unity TextMeshPro UI";
  else if (src.includes("textmeshpro") || src.includes("tmp_text")) base = "Unity TextMeshPro";
  else if (src.includes("unityengine.ui") || src.includes("ui.text")) base = "Unity UI Text";
  else if (src.includes("uielements")) base = "Unity UI Toolkit";
  else if (src.includes("uilabel")) base = "Unity NGUI Label";
  else if (src.includes("naninovel")) base = "Naninovel Dialogue";
  else if (src.includes("unity")) base = "Unity Text Hook";
  return `${base} #${idx + 1}`;
}

function hookTooltip(h) {
  return `${h.hookName || h.hookCode} · ctx ${h.ctx.toString(16)}`;
}
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h2 class="h2">Games &amp; Saved Hooks</h2>
        <p class="sub">Quick-attach to previously hooked games, manage saved embed hooks, or clear cached translations.</p>
      </div>
      <div class="actions">
        <button class="btn" type="button" @click="refreshSavedGames">
          <Icon name="RefreshCw" :size="13" />
          <span>Refresh</span>
        </button>
        <button class="btn" type="button" :disabled="cacheCount === 0 && !cacheCleared" @click="onClearCache">
          <Icon name="Trash" :size="13" />
          <span>{{ cacheCleared ? "Cache Cleared ✓" : `Clear Cache (${cacheCount})` }}</span>
        </button>
      </div>
    </div>

    <div v-if="!savedGames.length" class="empty-card">
      <p>No saved games yet. Attach to a running game in Hook Console and enable Embed on a hook to save it here.</p>
      <button class="btn primary" type="button" @click="emit('open-hooks')">Open Hook Console</button>
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
          <div class="hooks-head">
            <span class="hlbl">Saved Embed Hooks ({{ g.embedHooks?.length || 0 }})</span>
            <button
              v-if="g.embedHooks?.length"
              class="link-btn"
              type="button"
              @click="clearSavedGameHooks(g.exePath)"
            >
              Clear Hooks
            </button>
          </div>
          <div v-if="!g.embedHooks?.length" class="no-hooks">No saved embed hooks.</div>
          <div v-else class="hook-list">
            <div
              v-for="(h, idx) in g.embedHooks"
              :key="`${h.hookCode}:${h.ctx}:${h.ctx2}:${idx}`"
              class="hook-item"
            >
              <Tooltip :text="hookTooltip(h)" mono class="hname-tip">
                <span class="hname">{{ formatHookLabel(h, idx) }}</span>
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
        </div>
      </div>
    </div>
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
.link-btn:hover {
  color: var(--accent);
  text-decoration: underline;
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
</style>
