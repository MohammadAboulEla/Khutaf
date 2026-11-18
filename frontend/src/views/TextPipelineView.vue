<script setup>
import { computed, onMounted, ref, shallowRef, watch } from "vue";
import Icon from "../components/Icon.vue";
import Tooltip from "../components/Tooltip.vue";
import { useSession } from "../composables/useSession.js";

const {
  config,
  savedGames,
  cacheCount,
  pipelineTarget,
  refreshSavedGames,
  saveConfig,
  saveGamePipeline,
  listCacheEntries,
  saveCacheEntry,
  deleteCacheEntry,
  clearScopeCache,
  saveState,
} = useSession();

const preRules = ref([]);
const glossary = ref([]);
const postRules = ref([]);
const preSample = shallowRef("<b>Guide:</b> Welcome<br>to Wonderia!");
const postSample = shallowRef("الدليل: مرحباً بك في وونديريا!");

const cacheEntries = ref([]);
const cacheQuery = shallowRef("");
const cacheManualOnly = shallowRef(false);
const cacheLoading = shallowRef(false);
const showAddOverride = shallowRef(false);
const newSource = shallowRef("");
const newTarget = shallowRef("");
const savedRowKey = shallowRef("");

onMounted(() => {
  refreshSavedGames();
  refreshCache();
});

function loadRules() {
  if (pipelineTarget.value === "global") {
    const p = config.value?.pipeline;
    preRules.value = (p?.preRules || []).map((r) => ({ ...r }));
    glossary.value = (p?.glossary || []).map((g) => ({ ...g }));
    postRules.value = (p?.postRules || []).map((r) => ({ ...r }));
    return;
  }
  const game = savedGames.value.find((g) => g.exePath === pipelineTarget.value);
  const p = game?.pipeline;
  preRules.value = (p?.preRules || []).map((r) => ({ ...r }));
  glossary.value = (p?.glossary || []).map((g) => ({ ...g }));
  postRules.value = (p?.postRules || []).map((r) => ({ ...r }));
}

watch([config, savedGames, pipelineTarget], loadRules, { immediate: true });

function addRule(list) {
  list.push({ enabled: true, regex: false, pattern: "", replace: "" });
}

function addGlossaryTerm() {
  glossary.value.push({ enabled: true, caseSensitive: false, source: "", target: "" });
}

function removeRule(list, idx) {
  list.splice(idx, 1);
}

function onSave() {
  const payload = {
    preRules: preRules.value,
    postRules: postRules.value,
    glossary: glossary.value,
  };
  if (pipelineTarget.value === "global") {
    if (!config.value) return;
    saveConfig({ ...config.value, pipeline: payload });
  } else {
    saveGamePipeline(pipelineTarget.value, payload);
  }
}

function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function isASCIIWordChar(ch) {
  return /^[A-Za-z0-9_]$/.test(ch);
}

function applyRulesPreview(text, rules) {
  let out = text;
  let error = "";
  for (const r of rules) {
    if (!r.enabled || !r.pattern) continue;
    if (r.regex) {
      try {
        out = out.replace(new RegExp(r.pattern, "g"), r.replace || "");
      } catch (e) {
        error = String(e.message || e);
      }
    } else {
      out = out.split(r.pattern).join(r.replace || "");
    }
  }
  return { out, error };
}

function applyGlossaryPreview(text, entries) {
  const active = entries
    .map((e, idx) => ({
      enabled: e.enabled,
      caseSensitive: Boolean(e.caseSensitive),
      source: (e.source || "").trim(),
      target: (e.target || "").trim(),
      idx,
    }))
    .filter((e) => e.enabled && e.source && e.target)
    .sort((a, b) => [...b.source].length - [...a.source].length || a.idx - b.idx);

  let out = text;
  for (const e of active) {
    const chars = [...e.source];
    let pat = escapeRegex(e.source);
    if (chars.length && isASCIIWordChar(chars[0])) pat = `\\b${pat}`;
    if (chars.length && isASCIIWordChar(chars[chars.length - 1])) pat = `${pat}\\b`;
    const re = new RegExp(pat, e.caseSensitive ? "g" : "gi");
    out = out.replace(re, () => e.target);
  }
  return out;
}

const preview = computed(() => {
  const pre = applyRulesPreview(preSample.value, preRules.value);
  const afterGlossary = applyGlossaryPreview(pre.out, glossary.value);
  const post = applyRulesPreview(postSample.value, postRules.value);
  return {
    afterPre: afterGlossary,
    afterPost: post.out,
    error: pre.error || post.error,
  };
});

async function refreshCache() {
  cacheLoading.value = true;
  try {
    const rows = await listCacheEntries(pipelineTarget.value, cacheQuery.value, cacheManualOnly.value, 200);
    cacheEntries.value = Array.isArray(rows) ? rows.map((r) => ({ ...r })) : [];
  } finally {
    cacheLoading.value = false;
  }
}

let queryTimer = null;
watch(cacheQuery, () => {
  clearTimeout(queryTimer);
  queryTimer = setTimeout(refreshCache, 150);
});

watch([pipelineTarget, cacheManualOnly], () => {
  refreshCache();
});

function rowKey(entry) {
  return `${entry.gameUid}\0${entry.provider}\0${entry.sourceText}`;
}

async function onAddOverride() {
  const src = newSource.value.trim();
  if (!src) return;
  const gameUid = pipelineTarget.value === "global" ? "global" : pipelineTarget.value;
  await saveCacheEntry(gameUid, "manual", src, newTarget.value);
  newSource.value = "";
  newTarget.value = "";
  showAddOverride.value = false;
  await refreshCache();
}

async function onSaveCacheRow(entry) {
  await saveCacheEntry(entry.gameUid, entry.provider || "manual", entry.sourceText, entry.translated);
  const k = rowKey(entry);
  savedRowKey.value = k;
  setTimeout(() => {
    if (savedRowKey.value === k) savedRowKey.value = "";
  }, 1500);
  await refreshCache();
}

async function onDeleteCacheRow(entry) {
  await deleteCacheEntry(entry.gameUid, entry.provider, entry.sourceText);
  await refreshCache();
}

async function onClearAutoCache() {
  const scope = pipelineTarget.value === "global" ? "" : pipelineTarget.value;
  await clearScopeCache(scope, false);
  await refreshCache();
}

function formatScopeName(gameUid) {
  if (!gameUid || gameUid === "global") return "global";
  const parts = gameUid.split(/[\\/]/);
  return parts[parts.length - 1] || gameUid;
}
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h2 class="h2">Text Pipeline Rules</h2>
        <p class="sub">Global rules apply to every game; per-game rules run on top of global rules only for that game.</p>
      </div>
      <div class="toolbar-actions">
        <select v-model="pipelineTarget" class="scope-select">
          <option value="global">Global (All Games)</option>
          <option v-for="g in savedGames" :key="g.exePath" :value="g.exePath">
            {{ g.title }}{{ g.attached ? " (Attached)" : "" }}
          </option>
        </select>
        <button class="btn primary" type="button" @click="onSave">
          {{ saveState === "saving" ? "Saving…" : saveState === "saved" ? "Saved ✓" : "Save Rules" }}
        </button>
      </div>
    </div>

    <div class="sections">
      <div class="card">
        <div class="head">
          <div class="head-title">
            <span class="step">Step 1</span>
            <div>
              <h3 class="h3">Pre-Translation Rules</h3>
              <p class="sub">Strip noise or normalize text before sending it to the translator.</p>
            </div>
          </div>
          <button class="btn" type="button" @click="addRule(preRules)">+ Add Rule</button>
        </div>

        <div v-if="!preRules.length" class="empty">No pre-translation rules configured for this scope.</div>
        <div v-for="(r, idx) in preRules" :key="'pre-' + idx" class="rule-row">
          <Tooltip text="Enable rule">
            <input v-model="r.enabled" type="checkbox" />
          </Tooltip>
          <button
            type="button"
            class="mode-chip"
            :class="{ active: r.regex }"
            @click="r.regex = !r.regex"
          >
            {{ r.regex ? "Regex" : "Literal" }}
          </button>
          <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
          <span class="arrow">→</span>
          <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
          <button class="icon-btn" type="button" @click="removeRule(preRules, idx)">
            <Icon name="Trash" :size="13" />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="head">
          <div class="head-title">
            <span class="step">Step 2</span>
            <div>
              <h3 class="h3">Glossary &amp; Proper Nouns</h3>
              <p class="sub">Enforce consistent Arabic names before translation and inject matching terms into LLM prompts.</p>
            </div>
          </div>
          <button class="btn" type="button" @click="addGlossaryTerm">+ Add Term</button>
        </div>

        <div v-if="!glossary.length" class="empty">No glossary terms configured for this scope.</div>
        <div v-for="(g, idx) in glossary" :key="'gloss-' + idx" class="rule-row">
          <Tooltip text="Enable term">
            <input v-model="g.enabled" type="checkbox" />
          </Tooltip>
          <button
            type="button"
            class="mode-chip"
            :class="{ active: g.caseSensitive }"
            @click="g.caseSensitive = !g.caseSensitive"
          >
            {{ g.caseSensitive ? "Case" : "Any Case" }}
          </button>
          <input v-model="g.source" class="input" dir="auto" placeholder="Source term (e.g. Wonderia)…" />
          <span class="arrow">→</span>
          <input v-model="g.target" class="input ar" dir="rtl" placeholder="Arabic translation (e.g. وونديريا)…" />
          <button class="icon-btn" type="button" @click="removeRule(glossary, idx)">
            <Icon name="Trash" :size="13" />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="head">
          <div class="head-title">
            <span class="step">Step 3</span>
            <div>
              <h3 class="h3">Post-Translation Correction</h3>
              <p class="sub">Fix recurring translation quirks before Arabic shaping &amp; wrapping.</p>
            </div>
          </div>
          <button class="btn" type="button" @click="addRule(postRules)">+ Add Rule</button>
        </div>

        <div v-if="!postRules.length" class="empty">No post-translation rules configured for this scope.</div>
        <div v-for="(r, idx) in postRules" :key="'post-' + idx" class="rule-row">
          <Tooltip text="Enable rule">
            <input v-model="r.enabled" type="checkbox" />
          </Tooltip>
          <button
            type="button"
            class="mode-chip"
            :class="{ active: r.regex }"
            @click="r.regex = !r.regex"
          >
            {{ r.regex ? "Regex" : "Literal" }}
          </button>
          <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
          <span class="arrow">→</span>
          <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
          <button class="icon-btn" type="button" @click="removeRule(postRules, idx)">
            <Icon name="Trash" :size="13" />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="head">
          <div>
            <h3 class="h3">Live Rule Tester</h3>
            <p class="sub">Test Pre-Rules &amp; Glossary on raw game text (left) and Post-Rules on translated Arabic (right).</p>
          </div>
          <span v-if="preview.error" class="err">{{ preview.error }}</span>
        </div>
        <div class="preview-grid">
          <div class="pbox">
            <div class="plbl">Raw Game Text → After Pre-Rules &amp; Glossary</div>
            <input v-model="preSample" class="input mono pinput" dir="auto" placeholder="Raw game text (e.g. <b>Guide:</b> Hello<br>world)…" />
            <div class="pval mono" dir="auto">{{ preview.afterPre }}</div>
          </div>
          <div class="pbox">
            <div class="plbl">Translated Arabic → After Post-Rules</div>
            <input v-model="postSample" class="input pinput" dir="rtl" placeholder="Translated Arabic text…" />
            <div class="pval ar" dir="rtl">{{ preview.afterPost }}</div>
          </div>
        </div>
      </div>

      <div class="card">
        <div class="head cache-head">
          <div class="head-title">
            <span class="step">Cache</span>
            <div>
              <div class="cache-title-row">
                <h3 class="h3">Translation Memory (SQLite Cache)</h3>
                <span class="badge">{{ cacheCount }} total</span>
              </div>
              <p class="sub">Instant 0 ms persisted translations. Edited rows become permanent manual overrides.</p>
            </div>
          </div>
          <div class="cache-actions">
            <button class="btn" type="button" @click="showAddOverride = !showAddOverride">
              {{ showAddOverride ? "Cancel" : "+ Add Override" }}
            </button>
            <button class="btn inline-btn" type="button" :disabled="cacheLoading" @click="refreshCache">
              <Icon name="RefreshCw" :size="12" />
              <span>Refresh</span>
            </button>
            <button class="btn" type="button" @click="onClearAutoCache">Clear Auto Cache</button>
          </div>
        </div>

        <div class="cache-filters">
          <div class="search-wrap">
            <Icon name="Search" :size="13" />
            <input
              v-model="cacheQuery"
              class="search-input"
              dir="auto"
              placeholder="Search source or Arabic translation…"
            />
          </div>
          <button
            type="button"
            class="mode-chip"
            :class="{ active: cacheManualOnly }"
            @click="cacheManualOnly = !cacheManualOnly"
          >
            Manual Only
          </button>
        </div>

        <div v-if="showAddOverride" class="override-bar">
          <input v-model="newSource" class="input" dir="auto" placeholder="Exact source line…" />
          <span class="arrow">→</span>
          <input v-model="newTarget" class="input ar" dir="rtl" placeholder="Manual Arabic translation…" />
          <button class="btn primary" type="button" :disabled="!newSource.trim()" @click="onAddOverride">
            Save Override
          </button>
        </div>

        <div v-if="!cacheEntries.length" class="empty">
          {{ cacheLoading ? "Loading cached translations…" : "No cached translations match this filter." }}
        </div>
        <div v-else class="cache-list">
          <div v-for="entry in cacheEntries" :key="rowKey(entry)" class="cache-row">
            <div class="cache-meta">
              <Tooltip :text="entry.gameUid" mono>
                <span class="tag">{{ formatScopeName(entry.gameUid) }}</span>
              </Tooltip>
              <span class="tag">{{ entry.provider || "manual" }}</span>
              <span v-if="entry.manual" class="tag manual">Manual</span>
            </div>
            <Tooltip :text="entry.sourceText" class="cache-src-tip">
              <div class="cache-src" dir="auto">{{ entry.sourceText }}</div>
            </Tooltip>
            <input v-model="entry.translated" class="input ar" dir="rtl" placeholder="Arabic translation…" />
            <button class="btn" type="button" @click="onSaveCacheRow(entry)">
              {{ savedRowKey === rowKey(entry) ? "Saved ✓" : "Save" }}
            </button>
            <Tooltip text="Delete entry">
              <button class="icon-btn" type="button" @click="onDeleteCacheRow(entry)">
                <Icon name="Trash" :size="13" />
              </button>
            </Tooltip>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  overflow: auto;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.toolbar {
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
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.scope-select {
  height: 30px;
  padding: 0 10px;
  min-width: 240px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.sections {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.card {
  padding: 14px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.head-title {
  display: flex;
  align-items: center;
  gap: 10px;
}
.step {
  padding: 2px 8px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  flex: none;
}
.h2 {
  margin: 0 0 3px;
  font-size: 14px;
  font-weight: 600;
}
.h3 {
  margin: 0 0 2px;
  font-size: 13.5px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.rule-row {
  display: grid;
  grid-template-columns: auto auto 1fr auto 1fr auto;
  gap: 8px;
  align-items: center;
}
.mode-chip {
  height: 28px;
  min-width: 58px;
  padding: 0 8px;
  background: var(--paper);
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-family: var(--mono);
  font-size: 11px;
  cursor: pointer;
}
.mode-chip.active {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}
.arrow {
  color: var(--fg-muted);
  font-family: var(--mono);
  font-size: 12px;
}
.input {
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  min-width: 0;
}
.mono {
  font-family: var(--mono);
}
.btn {
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
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.icon-btn {
  height: 30px;
  width: 30px;
  background: var(--paper);
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 5px;
  cursor: pointer;
  display: grid;
  place-items: center;
}
.icon-btn:hover {
  color: #f87171;
}
.empty {
  color: var(--fg-muted);
  font-size: 12px;
  padding: 4px 0;
}
.preview-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.pbox {
  padding: 8px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.pinput {
  width: 100%;
  background: var(--bg-elev);
}
.plbl {
  font-size: 11px;
  color: var(--fg-muted);
  margin-bottom: 3px;
}
.pval {
  font-size: 12px;
  word-break: break-word;
}
.err {
  color: #f87171;
  font-family: var(--mono);
  font-size: 11px;
}
.ar {
  font-family: var(--arabic);
  font-size: 13px;
}
.cache-head {
  flex-wrap: wrap;
}
.cache-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.badge {
  padding: 1px 7px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-family: var(--mono);
  font-size: 11px;
  color: var(--fg-muted);
}
.cache-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.inline-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.cache-filters {
  display: flex;
  align-items: center;
  gap: 8px;
}
.search-wrap {
  flex: 1;
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--fg-muted);
}
.search-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.override-bar {
  display: grid;
  grid-template-columns: 1fr auto 1fr auto;
  gap: 8px;
  align-items: center;
  padding: 8px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.cache-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 380px;
  overflow-y: auto;
}
.cache-row {
  display: grid;
  grid-template-columns: auto minmax(160px, 0.9fr) minmax(180px, 1.1fr) auto auto;
  gap: 8px;
  align-items: center;
  padding: 6px 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
}
.cache-meta {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
  max-width: 210px;
}
.tag {
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  font-family: var(--mono);
  font-size: 10.5px;
  color: var(--fg-muted);
  white-space: nowrap;
  max-width: 110px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tag.manual {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}
.cache-src-tip {
  width: 100%;
  min-width: 0;
}
.cache-src {
  font-size: 12px;
  color: var(--fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
