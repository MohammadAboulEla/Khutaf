<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import Icon from "../Icon.vue";
import Tooltip from "../Tooltip.vue";
import { useSession } from "../../composables/useSession.js";
import { useI18n } from "../../composables/useI18n.js";
import { useHookBrowser } from "../../composables/useHookBrowser.js";
import {
  formatHookLabel,
  formatHookPrimaryTitle,
  formatRelativeAgo,
  getHookCategoryMeta,
} from "../../utils/hookLabels.js";

const props = defineProps({
  /** "simple" | "picker" | "advanced" */
  variant: { type: String, default: "picker" },
  active: { type: Boolean, default: true },
  autoFocusSearch: { type: Boolean, default: false },
  maxHeight: { type: String, default: "" },
});

const emit = defineEmits(["inspect-text"]);

const { t, lang } = useI18n();
const {
  hooks,
  hookOutputs,
  hookHistory,
  hookPulses,
  savedGames,
  status,
  config,
  setEmbed,
  setHookMuted,
  applyHookPreset,
  restoreEmbedHooks,
  addGlossaryTerm,
  removeSavedGameHook,
  refreshSavedGames,
  saveConfig,
} = useSession();

const {
  search,
  activeCategory,
  showSavedOnly,
  embedOnly,
  sortMode,
  frozen,
  expandedKeys,
  selectedIndex,
  nowTick,
  lastPresetUndo,
  savedOnlyHooks,
  counts,
  displayedHooks,
  needsSort,
  snapshotPinned,
  setCategory,
  cycleCategory,
  cycleSortMode,
  toggleFreeze,
  toggleExpand,
  isHookPulsing,
  moveSelection,
} = useHookBrowser({
  hooks,
  hookOutputs,
  hookHistory,
  hookPulses,
  savedGames,
  status,
  variant: props.variant,
});

const searchInputEl = ref(null);
const listEl = ref(null);
const presetBusy = shallowRef(false);
const presetNotice = shallowRef("");
let presetTimer = null;

// Inline quick-glossary composer state.
const glossaryDraft = ref(null); // { source, target, scopeGame, saving, saved }

watch(
  () => props.active,
  async (isActive) => {
    if (!isActive) return;
    snapshotPinned();
    if (props.autoFocusSearch) {
      await nextTick();
      searchInputEl.value?.focus?.();
    }
  },
  { immediate: true },
);

function showNotice(msg) {
  presetNotice.value = msg;
  if (presetTimer) clearTimeout(presetTimer);
  presetTimer = setTimeout(() => {
    presetNotice.value = "";
    presetTimer = null;
  }, 3000);
}

async function toggleVerboseHooks(val) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: { ...config.value.embed, verboseHooks: Boolean(val) },
  });
}

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

async function onToggleMute(h) {
  if (h.savedOnly) return;
  await setHookMuted(h.key, !h.muted);
}

async function onApplyPreset(preset) {
  if (presetBusy.value || !status.value.attached) return;
  presetBusy.value = true;
  try {
    const res = await applyHookPreset(preset);
    if (res) {
      lastPresetUndo.value = { preset, previousKeys: res.previousKeys || [] };
      showNotice(
        t("hbPresetApplied", {
          enabled: res.enabledNow || 0,
          total: res.totalActive || 0,
        }),
      );
    }
  } finally {
    presetBusy.value = false;
  }
}

async function onUndoPreset() {
  if (!lastPresetUndo.value || presetBusy.value) return;
  presetBusy.value = true;
  try {
    const prev = lastPresetUndo.value.previousKeys || [];
    const res = await restoreEmbedHooks(prev);
    lastPresetUndo.value = null;
    if (res) {
      showNotice(t("hbPresetUndone", { total: res.totalActive || 0 }));
    }
  } finally {
    presetBusy.value = false;
  }
}

function openQuickGlossary(rawText) {
  const clean = String(rawText || "")
    .replace(/<[^>]+>/g, "")
    .trim()
    .slice(0, 80);
  if (!clean) return;
  glossaryDraft.value = {
    source: clean,
    target: "",
    scopeGame: Boolean(status.value.attached && status.value.gamePath),
    saving: false,
    error: "",
  };
}

async function submitQuickGlossary() {
  const d = glossaryDraft.value;
  if (!d || d.saving || !d.source.trim() || !d.target.trim()) return;
  d.saving = true;
  d.error = "";
  try {
    await addGlossaryTerm(d.source.trim(), d.target.trim(), false, d.scopeGame);
    showNotice(t("hbGlossaryAdded", { source: d.source.trim(), target: d.target.trim() }));
    glossaryDraft.value = null;
  } catch (e) {
    d.error = String(e?.message || e);
    d.saving = false;
  }
}

async function copyText(text) {
  if (!text) return;
  try {
    await navigator.clipboard?.writeText(text);
    showNotice(t("hbCopied"));
  } catch {
    // ignore clipboard errors
  }
}

function scrollSelectedIntoView() {
  nextTick(() => {
    const el = listEl.value?.querySelector(`[data-hook-idx="${selectedIndex.value}"]`);
    el?.scrollIntoView?.({ block: "nearest" });
  });
}

function onKeydown(e) {
  if (!props.active || props.variant !== "picker") return;
  if (glossaryDraft.value) {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      glossaryDraft.value = null;
    }
    return;
  }
  const inSearch = document.activeElement === searchInputEl.value;
  if (e.key === "ArrowDown") {
    e.preventDefault();
    moveSelection(1);
    scrollSelectedIntoView();
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    moveSelection(-1);
    scrollSelectedIntoView();
  } else if (e.key === "Tab" && !e.ctrlKey && !e.altKey) {
    e.preventDefault();
    cycleCategory(e.shiftKey ? -1 : 1);
  } else if (e.key === " " && !inSearch) {
    const item = displayedHooks.value[selectedIndex.value];
    if (item && item.embeddable) {
      e.preventDefault();
      onToggleHook(item, !item.embedding);
    }
  } else if (e.key === "Enter") {
    const item = displayedHooks.value[selectedIndex.value];
    if (!item) return;
    e.preventDefault();
    if (e.ctrlKey || e.shiftKey) {
      toggleExpand(item.key);
    } else if (item.embeddable) {
      onToggleHook(item, !item.embedding);
    }
  } else if (e.key === "Escape" && inSearch && search.value) {
    e.preventDefault();
    e.stopPropagation();
    search.value = "";
  }
}

onMounted(() => window.addEventListener("keydown", onKeydown, true));
onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown, true);
  if (presetTimer) clearTimeout(presetTimer);
});

const sortModeLabel = () => {
  if (sortMode.value === "recent") return t("hbSortRecent");
  if (sortMode.value === "pinned") return t("sortCheckedTop");
  return t("hbSortRecommended");
};

function hookSubMeta(h) {
  const typeLabel = formatHookLabel(h, -1, lang.value);
  if (props.variant === "simple") {
    return h.objectPath ? typeLabel : "";
  }
  const parts = [];
  if (h.objectPath || h.path) parts.push(typeLabel);
  if (h.ctx) parts.push(`0x${h.ctx.toString(16)}`);
  if (props.variant === "advanced") {
    if (h.hookCode) parts.push(h.hookCode);
    if (h.hitCount) parts.push(`${h.hitCount}×`);
    if (h.updatesPerSec > 0) parts.push(`${h.updatesPerSec}/s`);
    if (typeof h.dialogueScore === "number") parts.push(`${h.dialogueScore}%`);
  }
  return parts.join(" · ");
}

defineExpose({
  focusSearch: () => searchInputEl.value?.focus?.(),
  snapshotPinned,
  activeCount: () => counts.value.active,
});
</script>

<template>
  <section class="hb-root" :class="[`v-${variant}`, { frozen }]">
    <!-- Top Controls: Presets & Toggles -->
    <div class="hb-controls">
      <div class="hb-presets-row">
        <div class="hb-presets">
          <Tooltip :text="t('hbPresetDialogueTip')">
            <button
              class="preset-btn primary"
              type="button"
              :disabled="presetBusy || !status.attached"
              @click="onApplyPreset('dialogue')"
            >
              <Icon name="Sparkles" :size="11" />
              <span>{{ t("hbPresetDialogue") }}</span>
            </button>
          </Tooltip>
          <Tooltip :text="t('hbPresetDialogueUiTip')">
            <button
              class="preset-btn"
              type="button"
              :disabled="presetBusy || !status.attached"
              @click="onApplyPreset('dialogue_ui')"
            >
              <Icon name="Layers" :size="11" />
              <span>{{ t("hbPresetDialogueUi") }}</span>
            </button>
          </Tooltip>
          <button
            v-if="counts.active > 0"
            class="preset-btn subtle"
            type="button"
            :disabled="presetBusy || !status.attached"
            @click="onApplyPreset('disable_all')"
          >
            <Icon name="X" :size="11" />
            <span>{{ t("hbPresetDisableAll") }}</span>
          </button>
          <button
            v-if="lastPresetUndo"
            class="preset-btn undo"
            type="button"
            :disabled="presetBusy"
            @click="onUndoPreset"
          >
            <Icon name="RotateCcw" :size="11" />
            <span>{{ t("hbUndo") }}</span>
          </button>
        </div>

        <div class="hb-right-tools">
          <Tooltip :text="t('hbFreezeTip')">
            <button
              class="tool-btn"
              :class="{ on: frozen }"
              type="button"
              @click="toggleFreeze"
            >
              <Icon :name="frozen ? 'Play' : 'Pause'" :size="11" />
              <span v-if="variant !== 'picker'">{{ frozen ? t("hbUnfreeze") : t("hbFreeze") }}</span>
            </button>
          </Tooltip>

          <Tooltip :text="t('hbSortCycleTip')">
            <button
              class="tool-btn"
              :class="{ dirty: sortMode === 'pinned' && needsSort }"
              type="button"
              @click="cycleSortMode"
            >
              <Icon name="ArrowUpDown" :size="11" />
              <span>{{ sortModeLabel() }}</span>
            </button>
          </Tooltip>

          <slot name="toolbar-extra" />
        </div>
      </div>

      <!-- Category Filter Chips -->
      <div class="hb-chips-row">
        <div class="hb-chips" role="tablist">
          <button
            type="button"
            class="cat-chip"
            :class="{ active: activeCategory === 'recommended' }"
            @click="setCategory('recommended')"
          >
            <span>{{ t("hbCatRecommended") }}</span>
            <span class="chip-count">{{ counts.recommended }}</span>
          </button>
          <button
            type="button"
            class="cat-chip"
            :class="{ active: activeCategory === 'active' }"
            @click="setCategory('active')"
          >
            <span>{{ t("hbCatActive") }}</span>
            <span class="chip-count">{{ counts.active }}</span>
          </button>
          <button
            type="button"
            class="cat-chip"
            :class="{ active: activeCategory === 'ui' }"
            @click="setCategory('ui')"
          >
            <span>{{ t("hbCatUI") }}</span>
            <span class="chip-count">{{ counts.ui }}</span>
          </button>
          <button
            type="button"
            class="cat-chip"
            :class="{ active: activeCategory === 'all' }"
            @click="setCategory('all')"
          >
            <span>{{ t("hbCatAll") }}</span>
            <span class="chip-count">{{ counts.all }}</span>
          </button>
          <button
            v-if="counts.noise > 0"
            type="button"
            class="cat-chip muted-chip"
            :class="{ active: activeCategory === 'noise' }"
            @click="setCategory('noise')"
          >
            <span>{{ t("hbCatNoise") }}</span>
            <span class="chip-count">{{ counts.noise }}</span>
          </button>
          <button
            v-if="counts.muted > 0"
            type="button"
            class="cat-chip muted-chip"
            :class="{ active: activeCategory === 'muted' }"
            @click="setCategory('muted')"
          >
            <span>{{ t("hbCatMuted") }}</span>
            <span class="chip-count">{{ counts.muted }}</span>
          </button>
        </div>

        <div class="hb-toggles">
          <Tooltip :text="t('verboseHooksTip')">
            <label
              class="mini-pill"
              :class="{ on: Boolean(config?.embed?.verboseHooks) }"
            >
              <input
                type="checkbox"
                class="sr-only"
                :checked="Boolean(config?.embed?.verboseHooks)"
                @change="toggleVerboseHooks($event.target.checked)"
              />
              <span class="pill-dot" />
              <span>{{ t("hbStaticUiShort") }}</span>
            </label>
          </Tooltip>

          <Tooltip v-if="savedOnlyHooks.length || showSavedOnly" :text="t('qhShowAllActiveTip')">
            <label
              class="mini-pill"
              :class="{ on: showSavedOnly }"
            >
              <input v-model="showSavedOnly" type="checkbox" class="sr-only" />
              <span class="pill-dot" />
              <span>{{ t("qhSavedBadge") }}</span>
              <span v-if="savedOnlyHooks.length" class="chip-count">+{{ savedOnlyHooks.length }}</span>
            </label>
          </Tooltip>

          <label v-if="variant === 'advanced'" class="mini-pill" :class="{ on: embedOnly }">
            <input v-model="embedOnly" type="checkbox" class="sr-only" />
            <span class="pill-dot" />
            <span>{{ t("hbEmbedOnlyShort") }}</span>
          </label>
        </div>
      </div>

      <!-- Search Bar (searches live text + history + path) -->
      <div class="hb-search-row">
        <div class="hb-search-wrap">
          <Icon name="Search" :size="12" class="search-ico" />
          <input
            ref="searchInputEl"
            v-model="search"
            class="hb-search-input"
            type="text"
            :placeholder="t('hbSearchPlaceholder')"
          />
          <span v-if="frozen" class="frozen-pill">{{ t("hbFrozenBadge") }}</span>
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

      <div v-if="presetNotice" class="hb-notice">{{ presetNotice }}</div>
    </div>

    <!-- Quick Glossary Inline Composer -->
    <div v-if="glossaryDraft" class="glossary-bar">
      <div class="glossary-head">
        <span class="glossary-title">
          <Icon name="BookOpen" :size="12" />
          <span>{{ t("hbQuickGlossaryTitle") }}</span>
        </span>
        <button class="icon-mini" type="button" @click="glossaryDraft = null">
          <Icon name="X" :size="12" />
        </button>
      </div>
      <div class="glossary-fields">
        <input
          v-model="glossaryDraft.source"
          class="g-input"
          dir="auto"
          :placeholder="t('hbGlossarySourcePh')"
        />
        <input
          v-model="glossaryDraft.target"
          class="g-input"
          dir="rtl"
          :placeholder="t('hbGlossaryTargetPh')"
          @keydown.enter.prevent="submitQuickGlossary"
        />
        <button
          class="preset-btn primary"
          type="button"
          :disabled="glossaryDraft.saving || !glossaryDraft.source.trim() || !glossaryDraft.target.trim()"
          @click="submitQuickGlossary"
        >
          <Icon name="Check" :size="11" />
          <span>{{ t("hbGlossarySave") }}</span>
        </button>
      </div>
      <div class="glossary-foot">
        <label class="g-scope">
          <input v-model="glossaryDraft.scopeGame" type="checkbox" :disabled="!status.attached" />
          <span>{{ t("hbGlossaryScopeGame") }}</span>
        </label>
        <span v-if="glossaryDraft.error" class="g-err">{{ glossaryDraft.error }}</span>
      </div>
    </div>

    <!-- Hook List Body -->
    <div ref="listEl" class="hb-list-scroll" :style="maxHeight ? { maxHeight } : undefined">
      <div v-if="!displayedHooks.length" class="hb-empty">
        <Icon name="Activity" :size="22" class="empty-ico" />
        <div class="empty-msg">
          {{
            activeCategory === "recommended" && counts.all > 0
              ? t("hbEmptyRecommended")
              : t("qhEmpty")
          }}
        </div>
        <div class="empty-actions">
          <button
            v-if="activeCategory !== 'all' && counts.all > 0"
            class="preset-btn"
            type="button"
            @click="setCategory('all')"
          >
            {{ t("hbShowAllCount", { count: counts.all }) }}
          </button>
          <button
            v-if="!showSavedOnly && savedOnlyHooks.length"
            class="preset-btn"
            type="button"
            @click="showSavedOnly = true"
          >
            {{ t("qhRevealSaved", { count: savedOnlyHooks.length }) }}
          </button>
        </div>
        <slot name="empty-extra" />
      </div>

      <div v-else class="hb-list">
        <div
          v-for="(h, idx) in displayedHooks"
          :key="h.key"
          :data-hook-idx="idx"
          class="hb-card"
          :class="{
            active: h.embedding,
            saved: h.savedOnly,
            muted: h.muted,
            selected: variant === 'picker' && selectedIndex === idx,
            pulsing: isHookPulsing(h.key),
          }"
          @click="selectedIndex = idx"
        >
          <div class="hb-row-main">
            <label class="hb-check-hit" @click.stop>
              <input
                type="checkbox"
                class="sr-only"
                :checked="h.embedding"
                :disabled="!h.embeddable || h.muted"
                @change="onToggleHook(h, $event.target.checked)"
              />
              <span class="custom-check" :class="{ checked: h.embedding, disabled: !h.embeddable || h.muted }">
                <Icon v-if="h.embedding" name="Check" :size="11" />
              </span>
            </label>

            <div class="hb-body" @click="h.embeddable && !h.muted && onToggleHook(h, !h.embedding)">
              <div class="hb-text-line">
                <div
                  class="hb-sample"
                  :class="{ empty: !h._currentText }"
                  dir="auto"
                >
                  {{ h._currentText || "—" }}
                </div>
                <div class="hb-badges">
                  <Tooltip v-if="isHookPulsing(h.key)" :text="t('hbLivePulseTip')">
                    <span class="pulse-dot" />
                  </Tooltip>
                  <span v-if="h.lastSeenMs" class="ago-tag">
                    {{ formatRelativeAgo(h.lastSeenMs, nowTick, lang) }}
                  </span>
                  <span
                    v-if="h.savedOnly"
                    class="cat-badge saved"
                  >{{ t("qhSavedBadge") }}</span>
                  <span
                    v-else
                    class="cat-badge"
                    :class="getHookCategoryMeta(h.category, lang).tone"
                  >{{ getHookCategoryMeta(h.category, lang).label }}</span>
                </div>
              </div>

              <div v-if="h._matchedHistory" class="hb-match-hint" dir="auto">
                <span class="match-lbl">{{ t("hbMatchedHistory") }}:</span>
                <span>{{ h._matchedHistory }}</span>
              </div>

              <div class="hb-meta-line">
                <Tooltip :text="h.objectPath || h.hookName || h.hookCode" mono>
                  <span class="hb-title">
                    {{ formatHookPrimaryTitle(h, lang) }}
                  </span>
                </Tooltip>
                <span v-if="hookSubMeta(h)" class="hb-sub">{{ hookSubMeta(h) }}</span>
              </div>
            </div>

            <div class="hb-row-actions" @click.stop>
              <Tooltip v-if="h._history?.length" :text="t('hbHistoryTip')">
                <button
                  class="row-ico-btn"
                  :class="{ on: Boolean(expandedKeys[h.key]) }"
                  type="button"
                  @click="toggleExpand(h.key)"
                >
                  <Icon name="Clock" :size="12" />
                  <span v-if="h._history.length > 1" class="ico-count">{{ h._history.length }}</span>
                </button>
              </Tooltip>

              <Tooltip
                v-if="h._currentText && (h.category === 'speaker' || variant === 'advanced')"
                :text="t('hbAddGlossaryTip')"
              >
                <button
                  class="row-ico-btn"
                  type="button"
                  @click="openQuickGlossary(h._currentText)"
                >
                  <Icon name="BookOpen" :size="12" />
                </button>
              </Tooltip>

              <Tooltip v-if="variant === 'advanced' && h._currentText" :text="t('hbInspectTip')">
                <button
                  class="row-ico-btn"
                  type="button"
                  @click="emit('inspect-text', h._currentText)"
                >
                  <Icon name="Wand" :size="12" />
                </button>
              </Tooltip>

              <Tooltip v-if="!h.savedOnly" :text="h.muted ? t('hbUnmuteTip') : t('hbMuteTip')">
                <button
                  class="row-ico-btn"
                  :class="{ warn: h.muted }"
                  type="button"
                  @click="onToggleMute(h)"
                >
                  <Icon :name="h.muted ? 'Eye' : 'EyeOff'" :size="12" />
                </button>
              </Tooltip>

              <slot name="row-actions" :hook="h" />
            </div>
          </div>

          <!-- Inline Expandable Temporary History Drawer -->
          <div v-if="expandedKeys[h.key]" class="hb-history-drawer">
            <div class="hist-head">
              <span>{{ t("hbHistoryTitle", { count: h._history.length }) }}</span>
              <span v-if="h.objectPath" class="hist-path">{{ h.objectPath }}</span>
            </div>
            <div class="hist-items">
              <div v-for="(item, hIdx) in h._history" :key="hIdx" class="hist-item">
                <span class="hist-ago">
                  {{ item.atMs ? formatRelativeAgo(item.atMs, nowTick, lang) : "—" }}
                </span>
                <span class="hist-text" dir="auto">{{ item.text }}</span>
                <div class="hist-acts">
                  <Tooltip :text="t('hbCopyTip')">
                    <button
                      class="icon-mini"
                      type="button"
                      @click="copyText(item.text)"
                    >
                      <Icon name="Copy" :size="11" />
                    </button>
                  </Tooltip>
                  <Tooltip :text="t('hbAddGlossaryTip')">
                    <button
                      class="icon-mini"
                      type="button"
                      @click="openQuickGlossary(item.text)"
                    >
                      <Icon name="Plus" :size="11" />
                    </button>
                  </Tooltip>
                  <Tooltip v-if="variant === 'advanced'" :text="t('hbInspectTip')">
                    <button
                      class="icon-mini"
                      type="button"
                      @click="emit('inspect-text', item.text)"
                    >
                      <Icon name="Wand" :size="11" />
                    </button>
                  </Tooltip>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.hb-root {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 7px;
  overflow: hidden;
}
.hb-root.v-picker {
  border: none;
  border-radius: 0;
  background: transparent;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  opacity: 0;
  pointer-events: none;
}

.hb-controls {
  display: flex;
  flex-direction: column;
  gap: 7px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.hb-root.v-simple .hb-controls {
  padding: 8px 10px;
}

.hb-presets-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}
.hb-presets {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px;
}
.hb-right-tools {
  display: flex;
  align-items: center;
  gap: 5px;
  margin-inline-start: auto;
}
.preset-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 25px;
  padding: 0 9px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  transition: background 0.14s, border-color 0.14s, color 0.14s;
}
.preset-btn:hover:not(:disabled) {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
}
.preset-btn.primary {
  background: color-mix(in srgb, var(--accent) 16%, var(--bg-elev));
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  color: var(--accent);
}
.preset-btn.primary:hover:not(:disabled) {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.preset-btn.subtle {
  color: var(--fg-muted);
}
.preset-btn.subtle:hover:not(:disabled) {
  color: #f87171;
  border-color: rgba(248, 113, 113, 0.45);
}
.preset-btn.undo {
  border-color: rgba(251, 191, 36, 0.55);
  background: rgba(251, 191, 36, 0.12);
  color: #fbbf24;
}
.preset-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.tool-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  height: 25px;
  padding: 0 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-elev);
  color: var(--fg-muted);
  font: inherit;
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
}
.tool-btn:hover {
  color: var(--fg);
}
.tool-btn.on {
  border-color: rgba(251, 191, 36, 0.65);
  background: rgba(251, 191, 36, 0.14);
  color: #fbbf24;
}
.tool-btn.dirty {
  border-color: var(--accent);
  color: var(--accent);
}

.hb-chips-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
}
.hb-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.cat-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg-muted);
  font: inherit;
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.14s, border-color 0.14s, color 0.14s;
}
.cat-chip:hover {
  color: var(--fg);
}
.cat-chip.active {
  background: color-mix(in srgb, var(--accent) 16%, var(--paper));
  border-color: color-mix(in srgb, var(--accent) 60%, var(--border));
  color: var(--fg);
  font-weight: 600;
}
.cat-chip.muted-chip {
  opacity: 0.78;
}
.chip-count {
  padding: 0 5px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-size: 10px;
  font-weight: 600;
}

.hb-toggles {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 5px;
}
.mini-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 23px;
  padding: 0 8px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg-muted);
  font-size: 10.5px;
  font-weight: 500;
  cursor: pointer;
  user-select: none;
}
.mini-pill.on {
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
.mini-pill.on .pill-dot {
  background: var(--accent);
  opacity: 1;
}

.hb-search-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.hb-search-wrap {
  flex: 1;
  min-width: 0;
  height: 29px;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 9px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elev);
}
.hb-search-wrap:focus-within {
  border-color: color-mix(in srgb, var(--accent) 65%, var(--border));
}
.search-ico {
  color: var(--fg-muted);
  flex: none;
}
.hb-search-input {
  flex: 1;
  min-width: 0;
  background: transparent;
  border: none;
  outline: none;
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.frozen-pill {
  padding: 1px 6px;
  border-radius: 4px;
  background: rgba(251, 191, 36, 0.16);
  color: #fbbf24;
  font-size: 10px;
  font-weight: 600;
  flex: none;
}
.clear-btn {
  display: grid;
  place-items: center;
  width: 18px;
  height: 18px;
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
.hb-notice {
  font-size: 11px;
  color: #4ade80;
  padding: 2px 4px 0;
}

/* Inline Quick Glossary Bar */
.glossary-bar {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--accent) 9%, var(--bg));
  border-bottom: 1px solid color-mix(in srgb, var(--accent) 40%, var(--border));
}
.glossary-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.glossary-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11.5px;
  font-weight: 600;
  color: var(--accent);
}
.glossary-fields {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.g-input {
  flex: 1 1 130px;
  min-width: 0;
  height: 26px;
  padding: 0 8px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.glossary-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
}
.g-scope {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--fg-muted);
  cursor: pointer;
}
.g-err {
  color: #f87171;
}

/* List Body */
.hb-list-scroll {
  flex: 1;
  min-height: 140px;
  overflow: auto;
  padding: 8px 10px;
}
.hb-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 24px 14px;
  text-align: center;
  color: var(--fg-muted);
}
.empty-ico {
  opacity: 0.55;
}
.empty-msg {
  font-size: 12px;
}
.empty-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.hb-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.hb-card {
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--paper);
  transition: border-color 0.16s, background 0.16s, box-shadow 0.2s;
}
.hb-card:hover {
  border-color: color-mix(in oklab, var(--fg) 22%, var(--border));
}
.hb-card.active {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 7%, var(--paper));
}
.hb-card.selected {
  outline: 2px solid color-mix(in srgb, var(--accent) 75%, transparent);
  outline-offset: -1px;
}
.hb-card.pulsing {
  box-shadow: 0 0 0 1px rgba(34, 197, 94, 0.45);
}
.hb-card.muted {
  opacity: 0.65;
}

.hb-row-main {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 8px 10px;
}
.hb-root.v-picker .hb-row-main {
  padding: 7px 9px;
}

.hb-check-hit {
  padding-top: 2px;
  cursor: pointer;
  flex: none;
}
.custom-check {
  display: grid;
  place-items: center;
  width: 17px;
  height: 17px;
  border-radius: 4px;
  border: 1.5px solid color-mix(in oklab, var(--fg) 30%, var(--border));
  background: var(--bg-elev);
  color: #fff;
  transition: background 0.12s, border-color 0.12s;
}
.custom-check.checked {
  background: var(--accent);
  border-color: var(--accent);
}
.custom-check.disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.hb-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
  cursor: pointer;
}
.hb-text-line {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}
.hb-sample {
  flex: 1;
  min-width: 0;
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
.hb-root.v-advanced .hb-sample {
  -webkit-line-clamp: 3;
}
.hb-sample.empty {
  color: var(--fg-muted);
  font-weight: 400;
  font-style: italic;
}

.hb-badges {
  display: flex;
  align-items: center;
  gap: 5px;
  flex: none;
  padding-top: 2px;
}
.pulse-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #22c55e;
  box-shadow: 0 0 8px rgba(34, 197, 94, 0.85);
}
.ago-tag {
  font-size: 10px;
  color: var(--fg-muted);
  white-space: nowrap;
}
.cat-badge {
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  font-weight: 600;
  white-space: nowrap;
}
.cat-badge.dialogue {
  background: color-mix(in srgb, var(--accent) 20%, transparent);
  color: var(--accent);
}
.cat-badge.speaker {
  background: rgba(20, 184, 166, 0.18);
  color: #2dd4bf;
}
.cat-badge.ui {
  background: rgba(59, 130, 246, 0.16);
  color: #60a5fa;
}
.cat-badge.noise {
  background: rgba(245, 158, 11, 0.16);
  color: #fbbf24;
}
.cat-badge.unknown,
.cat-badge.saved {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg-muted);
}

.hb-match-hint {
  font-size: 11px;
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 10%, transparent);
  padding: 2px 6px;
  border-radius: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.match-lbl {
  font-weight: 600;
  margin-inline-end: 4px;
}

.hb-meta-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  font-size: 11px;
  color: var(--fg-muted);
}
.hb-title {
  font-weight: 500;
  color: color-mix(in oklab, var(--fg) 78%, var(--fg-muted));
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}
.hb-sub {
  font-family: var(--mono);
  font-size: 10px;
  opacity: 0.85;
}

.hb-row-actions {
  display: flex;
  align-items: center;
  gap: 3px;
  flex: none;
}
.row-ico-btn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  height: 24px;
  min-width: 24px;
  justify-content: center;
  padding: 0 5px;
  border: 1px solid transparent;
  border-radius: 5px;
  background: transparent;
  color: var(--fg-muted);
  font: inherit;
  font-size: 10px;
  cursor: pointer;
}
.row-ico-btn:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg);
}
.row-ico-btn.on {
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  border-color: color-mix(in srgb, var(--accent) 40%, transparent);
  color: var(--accent);
}
.row-ico-btn.warn {
  color: #fbbf24;
}
.ico-count {
  font-weight: 600;
}

/* Expandable History Drawer */
.hb-history-drawer {
  border-top: 1px dashed var(--border);
  padding: 7px 10px 8px;
  background: color-mix(in oklab, var(--bg) 60%, var(--paper));
  border-radius: 0 0 7px 7px;
}
.hist-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 10.5px;
  font-weight: 600;
  color: var(--fg-muted);
  margin-bottom: 5px;
}
.hist-path {
  font-family: var(--mono);
  font-size: 10px;
  opacity: 0.8;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hist-items {
  display: flex;
  flex-direction: column;
  gap: 4px;
  max-height: 170px;
  overflow: auto;
}
.hist-item {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding: 4px 6px;
  border-radius: 4px;
  background: var(--bg-elev);
  font-size: 11.5px;
}
.hist-ago {
  flex: none;
  font-size: 10px;
  color: var(--fg-muted);
  min-width: 42px;
  padding-top: 1px;
}
.hist-text {
  flex: 1;
  min-width: 0;
  color: var(--fg);
  white-space: pre-wrap;
  word-break: break-word;
}
.hist-acts {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
}
.icon-mini {
  display: grid;
  place-items: center;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 4px;
  background: transparent;
  color: var(--fg-muted);
  cursor: pointer;
}
.icon-mini:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg);
}
</style>
