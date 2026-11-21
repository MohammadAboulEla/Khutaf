<script setup>
import { computed, ref, shallowRef } from "vue";
import Icon from "./Icon.vue";
import Tooltip from "./Tooltip.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  translatorConfig: { type: Object, default: null },
  compact: { type: Boolean, default: false },
  activeProvider: { type: String, default: "" },
  fallbackProvider: { type: String, default: "" },
});

const emit = defineEmits(["select-provider", "select-fallback", "benchmark-done"]);

const {
  config,
  benchmarkItems,
  benchmarkRunning,
  benchmarkTotal,
  benchmarkTranslators,
  cancelBenchmark,
} = useSession();

const { t } = useI18n();

const mode = shallowRef("single");
const customText = shallowRef("[Test] Welcome, Hero! If you can read this in Arabic, your translator is working.");
const includeTraditional = shallowRef(true);
const includeLlm = shallowRef(false);
const includeOfflineLlm = shallowRef(false);
const sortBy = shallowRef("speed");
const showShaped = shallowRef(false);
const expanded = ref({});

const progressPct = computed(() => {
  if (!benchmarkTotal.value) return 0;
  return Math.min(100, Math.round((benchmarkItems.value.length / benchmarkTotal.value) * 100));
});

function statusRank(st) {
  switch (st) {
    case "ok":
      return 0;
    case "partial":
      return 1;
    case "timeout":
      return 2;
    case "error":
      return 3;
    case "skipped":
      return 4;
    case "cancelled":
      return 5;
    default:
      return 6;
  }
}

const sortedItems = computed(() => {
  const list = [...benchmarkItems.value];
  list.sort((a, b) => {
    const ra = statusRank(a.status);
    const rb = statusRank(b.status);
    if (ra !== rb) return ra - rb;
    if (ra <= 1) {
      if (sortBy.value === "quality") {
        if (a.qualityScore !== b.qualityScore) return b.qualityScore - a.qualityScore;
        if (a.avgMs !== b.avgMs) return a.avgMs - b.avgMs;
      } else {
        if (a.avgMs !== b.avgMs) return a.avgMs - b.avgMs;
        if (a.qualityScore !== b.qualityScore) return b.qualityScore - a.qualityScore;
      }
    }
    return a.label.localeCompare(b.label);
  });
  return list;
});

function pickBestProviders(items) {
  const working = (items || []).filter((x) => x.status === "ok" || x.status === "partial");
  working.sort((a, b) => {
    const sa = a.status === "ok" ? 0 : 1;
    const sb = b.status === "ok" ? 0 : 1;
    if (sa !== sb) return sa - sb;
    const safeA = a.hasArabic && a.tagsPreserved && a.placeholdersPreserved ? 0 : 1;
    const safeB = b.hasArabic && b.tagsPreserved && b.placeholdersPreserved ? 0 : 1;
    if (safeA !== safeB) return safeA - safeB;
    if (a.avgMs !== b.avgMs) return a.avgMs - b.avgMs;
    return (b.qualityScore || 0) - (a.qualityScore || 0);
  });
  const first = working[0] || null;
  const second = working.find((x) => x.providerId !== first?.providerId) || null;
  return {
    bestPrimary: first?.providerId || "",
    bestPrimaryLabel: first?.label || "",
    bestFallback: second?.providerId || "",
    bestFallbackLabel: second?.label || "",
  };
}

async function onRun() {
  expanded.value = {};
  const cfg = props.translatorConfig || config.value?.translator;
  const sum = await benchmarkTranslators({
    mode: props.compact ? "single" : mode.value,
    text: customText.value,
    includeTraditional: props.compact ? true : includeTraditional.value,
    includeLlm: props.compact ? false : includeLlm.value,
    includeOfflineLlm: props.compact ? false : includeOfflineLlm.value,
    timeoutMs: 10000,
    config: cfg,
  });
  if (sum && !sum.cancelled) {
    emit("benchmark-done", pickBestProviders(sum.items || benchmarkItems.value));
  }
}

function toggleExpand(id) {
  expanded.value[id] = !expanded.value[id];
}

function statusLabel(item) {
  switch (item.status) {
    case "ok":
      return t("benchStatusOk");
    case "partial":
      return `${t("benchStatusPartial")} (${item.succeeded}/${item.totalSamples})`;
    case "timeout":
      return t("benchStatusTimeout");
    case "error":
      return t("benchStatusError");
    case "skipped":
      return t("benchStatusSkipped");
    case "cancelled":
      return t("benchStatusCancelled");
    default:
      return item.status;
  }
}

function speedLabel(tier) {
  if (tier === "fast") return t("benchSpeedFast");
  if (tier === "medium") return t("benchSpeedMedium");
  if (tier === "slow") return t("benchSpeedSlow");
  return "";
}
</script>

<template>
  <div class="bench-wrap" :class="{ compact: props.compact }">
    <div v-if="!props.compact" class="bench-head">
      <div>
        <h2 class="h2">{{ t("benchTitle") }}</h2>
        <p class="sub">{{ t("benchSubtitle") }}</p>
      </div>
    </div>

    <div class="controls">
      <div v-if="!props.compact" class="seg-row">
        <div class="seg">
          <button
            type="button"
            class="seg-btn"
            :class="{ active: mode === 'single' }"
            @click="mode = 'single'"
          >
            {{ t("benchModeSingle") }}
          </button>
          <button
            type="button"
            class="seg-btn"
            :class="{ active: mode === 'pack' }"
            @click="mode = 'pack'"
          >
            {{ t("benchModePack") }}
          </button>
        </div>

        <div class="scopes">
          <label class="chk">
            <input v-model="includeTraditional" type="checkbox" />
            <span>{{ t("benchScopeTrad") }}</span>
          </label>
          <label class="chk">
            <input v-model="includeLlm" type="checkbox" />
            <span>{{ t("benchScopeLLM") }}</span>
          </label>
          <label class="chk">
            <input v-model="includeOfflineLlm" type="checkbox" />
            <span>{{ t("benchScopeOffline") }}</span>
          </label>
        </div>
      </div>

      <div class="run-row">
        <input
          v-if="mode === 'single' || props.compact"
          v-model="customText"
          class="input flex1"
          dir="auto"
        />
        <div v-else class="pack-note flex1">{{ t("benchPackPreview") }}</div>

        <button
          v-if="!benchmarkRunning"
          class="btn primary"
          type="button"
          :disabled="!props.compact && !includeTraditional && !includeLlm && !includeOfflineLlm"
          @click="onRun"
        >
          <Icon name="Zap" :size="13" />
          <span>{{ t("benchRun") }}</span>
        </button>
        <button v-else class="btn danger" type="button" @click="cancelBenchmark">
          <Icon name="Stop" :size="13" />
          <span>{{ t("benchCancel") }}</span>
        </button>
      </div>

      <div v-if="benchmarkRunning" class="progress-wrap">
        <div class="progress-track">
          <div class="progress-fill" :style="{ width: `${progressPct}%` }" />
        </div>
        <span class="progress-lbl">
          {{ t("benchProgress", { done: benchmarkItems.length, total: benchmarkTotal }) }}
        </span>
      </div>

      <div v-if="sortedItems.length" class="sort-bar">
        <div class="seg small">
          <button
            type="button"
            class="seg-btn"
            :class="{ active: sortBy === 'speed' }"
            @click="sortBy = 'speed'"
          >
            {{ t("benchSortSpeed") }}
          </button>
          <button
            type="button"
            class="seg-btn"
            :class="{ active: sortBy === 'quality' }"
            @click="sortBy = 'quality'"
          >
            {{ t("benchSortQuality") }}
          </button>
        </div>

        <label v-if="!props.compact" class="chk">
          <input v-model="showShaped" type="checkbox" />
          <span>{{ t("benchShowShaped") }}</span>
        </label>
      </div>
    </div>

    <div v-if="!sortedItems.length && !benchmarkRunning" class="empty">
      {{ t("benchEmpty") }}
    </div>

    <div v-else-if="sortedItems.length" class="table-scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th>{{ t("benchColProvider") }}</th>
            <th>{{ t("benchColStatus") }}</th>
            <th>{{ t("benchColSpeed") }}</th>
            <th v-if="!props.compact">{{ t("benchColQuality") }}</th>
            <th>{{ t("benchColOutput") }}</th>
            <th>{{ t("benchColAction") }}</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="item in sortedItems" :key="item.providerId">
            <tr :class="{ selected: activeProvider === item.providerId }">
              <td class="col-prov">
                <div class="prov-name">{{ item.label }}</div>
                <span class="cat-pill">{{ item.category }}</span>
              </td>

              <td class="col-status">
                <Tooltip :text="item.error || ''" mono>
                  <span
                    class="st-pill"
                    :class="[item.status, { hasErr: Boolean(item.error) }]"
                  >{{ statusLabel(item) }}</span>
                </Tooltip>
              </td>

              <td class="col-speed">
                <template v-if="item.succeeded > 0">
                  <div class="ms-val" dir="ltr">{{ item.avgMs }} ms</div>
                  <span v-if="item.speedTier" class="tier" :class="item.speedTier">
                    {{ speedLabel(item.speedTier) }}
                  </span>
                </template>
                <span v-else class="muted">—</span>
              </td>

              <td v-if="!props.compact" class="col-qual">
                <template v-if="item.succeeded > 0">
                  <div class="score-pill">{{ t("benchScoreLabel", { score: item.qualityScore }) }}</div>
                  <div class="q-tags">
                    <span class="q-mini" :class="item.hasArabic ? 'ok' : 'bad'">
                      {{ item.hasArabic ? t("benchArabicOk") : t("benchArabicBad") }}
                    </span>
                    <span class="q-mini" :class="item.tagsPreserved ? 'ok' : 'bad'">
                      {{ item.tagsPreserved ? t("benchTagsOk") : t("benchTagsBad") }}
                    </span>
                    <span class="q-mini" :class="item.placeholdersPreserved ? 'ok' : 'bad'">
                      {{ item.placeholdersPreserved ? t("benchVarsOk") : t("benchVarsBad") }}
                    </span>
                  </div>
                </template>
                <span v-else class="muted">—</span>
              </td>

              <td class="col-out">
                <template v-if="item.succeeded > 0">
                  <div v-if="!showShaped" class="ar-out" dir="rtl">{{ item.sampleOutput }}</div>
                  <div v-else class="ar-out shaped" dir="ltr">{{ item.sampleShaped }}</div>
                  <button
                    v-if="item.samples && item.samples.length > 1"
                    type="button"
                    class="link-btn"
                    @click="toggleExpand(item.providerId)"
                  >
                    {{ expanded[item.providerId] ? t("benchDetailsHide") : t("benchDetailsShow", { count: item.samples.length }) }}
                  </button>
                </template>
                <span v-else class="muted">—</span>
              </td>

              <td class="col-act">
                <div class="act-stack">
                  <button
                    type="button"
                    class="btn small"
                    :class="activeProvider === item.providerId ? 'active-btn' : 'primary'"
                    :disabled="activeProvider === item.providerId || item.status === 'skipped'"
                    @click="emit('select-provider', item.providerId)"
                  >
                    {{ activeProvider === item.providerId ? t("benchActiveNow") : t("benchUseThis") }}
                  </button>
                  <button
                    type="button"
                    class="btn small"
                    :class="fallbackProvider === item.providerId ? 'fb-btn' : ''"
                    :disabled="activeProvider === item.providerId || fallbackProvider === item.providerId || item.status === 'skipped'"
                    @click="emit('select-fallback', item.providerId)"
                  >
                    {{ fallbackProvider === item.providerId ? t("benchFallbackNow") : t("benchSetFallback") }}
                  </button>
                </div>
              </td>
            </tr>

            <tr v-if="expanded[item.providerId] && item.samples?.length > 1" class="detail-row">
              <td :colspan="props.compact ? 5 : 6">
                <div class="sample-list">
                  <div v-for="(sm, si) in item.samples" :key="si" class="sample-item">
                    <div class="sample-src" dir="ltr">
                      <span>{{ sm.source }}</span>
                      <span class="muted">({{ sm.elapsedMs }} ms)</span>
                    </div>
                    <div v-if="sm.error" class="err-hint">{{ sm.error }}</div>
                    <div v-else class="ar-out" dir="rtl">{{ showShaped ? sm.shaped : sm.translated }}</div>
                  </div>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.bench-wrap {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.bench-head .h2 {
  margin: 0 0 4px;
  font-size: 14px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.controls {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.seg-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
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
.scopes {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--fg);
  cursor: pointer;
}
.run-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.input {
  height: 32px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  min-width: 220px;
}
.pack-note {
  padding: 6px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
  color: var(--fg-muted);
}
.flex1 {
  flex: 1;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
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
.btn.active-btn {
  background: color-mix(in oklab, #16a34a 20%, transparent);
  border-color: rgba(74, 222, 128, 0.4);
  color: #4ade80;
}
.btn.fb-btn {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  border-color: color-mix(in oklab, var(--accent) 50%, transparent);
  color: var(--accent);
}
.act-stack {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.progress-wrap {
  display: flex;
  align-items: center;
  gap: 10px;
}
.progress-track {
  flex: 1;
  height: 6px;
  background: var(--paper);
  border-radius: 3px;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: var(--accent);
  transition: width 150ms ease;
}
.progress-lbl {
  font-size: 11.5px;
  color: var(--fg-muted);
  white-space: nowrap;
}
.sort-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.empty {
  padding: 18px;
  text-align: center;
  color: var(--fg-muted);
  font-size: 12px;
  background: var(--paper);
  border: 1px dashed var(--border);
  border-radius: 6px;
}
.table-scroll {
  overflow-x: auto;
  max-height: 440px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--paper);
}
.compact .table-scroll {
  max-height: 250px;
}
.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.tbl th,
.tbl td {
  padding: 8px 10px;
  border-bottom: 1px solid var(--border);
  vertical-align: top;
  text-align: start;
}
.compact .tbl th,
.compact .tbl td {
  padding: 7px 8px;
}
.col-prov,
.col-status,
.col-speed,
.col-act {
  width: 1%;
  white-space: nowrap;
}
.tbl th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--bg-elev);
  color: var(--fg-muted);
  font-size: 11px;
  font-weight: 600;
}
tr.selected {
  background: color-mix(in oklab, var(--accent) 8%, transparent);
}
.prov-name {
  font-weight: 600;
}
.cat-pill {
  display: inline-block;
  margin-top: 2px;
  font-family: var(--mono);
  font-size: 10px;
  color: var(--fg-muted);
}
.st-pill {
  display: inline-block;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  white-space: nowrap;
}
.st-pill.hasErr {
  cursor: help;
}
.st-pill.ok {
  background: color-mix(in oklab, #16a34a 20%, transparent);
  color: #4ade80;
}
.st-pill.partial {
  background: color-mix(in oklab, #d97706 20%, transparent);
  color: #fbbf24;
}
.st-pill.timeout,
.st-pill.error {
  background: color-mix(in oklab, #dc2626 20%, transparent);
  color: #f87171;
}
.st-pill.skipped,
.st-pill.cancelled {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg-muted);
}
.err-hint {
  margin-top: 3px;
  font-size: 10.5px;
  color: #f87171;
  max-width: 170px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ms-val {
  font-family: var(--mono);
  font-weight: 600;
  white-space: nowrap;
}
.tier {
  display: inline-block;
  margin-top: 2px;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 10px;
}
.tier.fast {
  background: color-mix(in oklab, #16a34a 20%, transparent);
  color: #4ade80;
}
.tier.medium {
  background: color-mix(in oklab, #d97706 20%, transparent);
  color: #fbbf24;
}
.tier.slow {
  background: color-mix(in oklab, #dc2626 20%, transparent);
  color: #f87171;
}
.score-pill {
  font-weight: 600;
  font-size: 11px;
  margin-bottom: 3px;
}
.q-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
}
.q-mini {
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 10px;
}
.q-mini.ok {
  background: color-mix(in oklab, #16a34a 15%, transparent);
  color: #4ade80;
}
.q-mini.bad {
  background: color-mix(in oklab, #dc2626 18%, transparent);
  color: #f87171;
}
.col-out {
  min-width: 0;
  width: auto;
}
.ar-out {
  font-family: var(--arabic);
  font-size: 13.5px;
  line-height: 1.45;
  word-break: break-word;
}
.ar-out.shaped {
  unicode-bidi: bidi-override;
  direction: ltr;
}
.link-btn {
  margin-top: 4px;
  padding: 0;
  border: none;
  background: none;
  color: var(--accent);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.muted {
  color: var(--fg-muted);
}
.detail-row td {
  background: var(--bg-elev);
}
.sample-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.sample-item {
  padding: 6px 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
}
.sample-src {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
  color: var(--fg-muted);
  margin-bottom: 3px;
}
</style>
