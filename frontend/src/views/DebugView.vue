<script setup>
import { computed, shallowRef } from "vue";
import { useSession } from "../composables/useSession.js";

const {
  status,
  config,
  debugTraces,
  telemetryEvents,
  functionStats,
  droppedEvents,
  setDebugMode,
  clearDebugTraces,
  clearTelemetry,
  exportDiagnosticBundle,
} = useSession();

const SUBSYSTEMS = [
  "ALL",
  "HOOK_NATIVE",
  "UNITY_FONT",
  "IPC_PIPE",
  "PIPELINE_CACHE",
  "TRANSLATOR",
  "ARABIC_SHAPER",
];

const LEVELS = [
  { id: "all", label: "ALL" },
  { id: "trace", label: "TRACE+" },
  { id: "info", label: "INFO+" },
  { id: "warn", label: "WARN+" },
  { id: "error", label: "ERROR" },
];

const LEVEL_RANK = { trace: 0, debug: 1, info: 2, warn: 3, error: 4 };

const selectedSubsystem = shallowRef("ALL");
const selectedLevel = shallowRef("all");
const selectedFunc = shallowRef("");
const searchQuery = shallowRef("");
const paused = shallowRef(false);
const showMatrix = shallowRef(true);
const showStream = shallowRef(true);
const exportMsg = shallowRef("");

const timeoutMs = computed(() => config.value?.embed?.timeoutMs || 2000);

const healthSummary = computed(() => {
  let totalCalls = 0;
  let panics = 0;
  let embedAvgUs = 0;
  let cacheHits = 0;
  let cacheTotal = 0;

  for (const st of functionStats.value) {
    totalCalls += st.calls || 0;
    if (st.func === "il2cpp::detour" || st.func === "mono::detour") {
      panics += st.errors || 0;
    }
    if (st.func === "Session.handleEmbed") {
      embedAvgUs = st.avgDurUs || 0;
    }
  }

  for (const ev of telemetryEvents.value) {
    if (ev.func === "Cache.Get" && ev.fields?.tier) {
      cacheTotal++;
      if (ev.fields.tier !== "miss") cacheHits++;
    }
  }

  const hitRatio = cacheTotal > 0 ? Math.round((cacheHits / cacheTotal) * 100) : 0;
  return {
    totalCalls,
    hitRatio,
    embedAvgUs,
    panics,
    dropped: droppedEvents.value || 0,
  };
});

const filteredEvents = computed(() => {
  if (paused.value && cachedSnapshot.value.length) {
    return applyFilters(cachedSnapshot.value);
  }
  return applyFilters(telemetryEvents.value);
});

const cachedSnapshot = shallowRef([]);

function togglePause() {
  if (!paused.value) {
    cachedSnapshot.value = telemetryEvents.value;
    paused.value = true;
  } else {
    paused.value = false;
    cachedSnapshot.value = [];
  }
}

function applyFilters(list) {
  const sub = selectedSubsystem.value;
  const lvl = selectedLevel.value;
  const minRank = lvl === "all" ? -1 : (LEVEL_RANK[lvl] ?? 0);
  const fn = selectedFunc.value;
  const q = searchQuery.value.trim().toLowerCase();

  return list.filter((ev) => {
    if (sub !== "ALL" && ev.subsystem !== sub) return false;
    if (minRank >= 0 && (LEVEL_RANK[ev.level] ?? 2) < minRank) return false;
    if (fn && ev.func !== fn) return false;
    if (q) {
      const hay = `${ev.func} ${ev.msg} ${ev.fields?.key || ""}`.toLowerCase();
      if (!hay.includes(q)) return false;
    }
    return true;
  });
}

function toggleFuncFilter(fnName) {
  selectedFunc.value = selectedFunc.value === fnName ? "" : fnName;
}

function statHealthClass(st) {
  if (st.errors > 0 || st.timeouts > 0) return "dot-red";
  if (st.maxDurUs >= timeoutMs.value * 800) return "dot-amber";
  return "dot-green";
}

function durationBadgeClass(ev) {
  if (ev.level === "error" || ev.level === "warn") {
    return ev.level === "error" ? "badge-red" : "badge-amber";
  }
  const ms = (ev.durationUs || 0) / 1000;
  if (ms > timeoutMs.value * 0.8) return "badge-amber";
  if (ms >= 15) return "badge-blue";
  return "badge-green";
}

function waterfallSegments(ev) {
  const f = ev.fields;
  if (!f || typeof f.totalUs !== "number" || f.totalUs <= 0) return null;
  const total = f.totalUs;
  const pct = (v) => Math.max(0, Math.min(100, ((v || 0) / total) * 100));
  return [
    { name: "Pre", us: f.preRulesUs || 0, w: pct(f.preRulesUs), cls: "wf-pre" },
    { name: "Trans", us: f.translateUs || 0, w: pct(f.translateUs), cls: "wf-trans" },
    { name: "Post", us: f.postRulesUs || 0, w: pct(f.postRulesUs), cls: "wf-post" },
    { name: "Shape", us: f.shapeUs || 0, w: pct(f.shapeUs), cls: "wf-shape" },
    { name: "IPC", us: f.callbackUs || 0, w: pct(f.callbackUs), cls: "wf-cb" },
  ];
}

function fmtUs(us) {
  if (!us) return "0 µs";
  if (us >= 1000) return `${(us / 1000).toFixed(2)} ms`;
  return `${us} µs`;
}

async function handleExport() {
  exportMsg.value = "Exporting…";
  const path = await exportDiagnosticBundle();
  if (path) {
    exportMsg.value = `Saved: ${path}`;
    setTimeout(() => {
      exportMsg.value = "";
    }, 4000);
  } else {
    exportMsg.value = "";
  }
}

function handleClearAll() {
  clearTelemetry();
  clearDebugTraces();
  selectedFunc.value = "";
}
</script>

<template>
  <div class="page">
    <div class="card">
      <!-- Panel A: Top Health Bar & Controls -->
      <div class="head">
        <div>
          <h2 class="h2">System Telemetry &amp; Function Monitor</h2>
          <p class="sub">
            {{
              status.debugMode
                ? "Verbose Trace ON: capturing full TRACE/DEBUG spans & pipeline waterfalls."
                : "Verbose Trace OFF (zero overhead): capturing INFO, WARN, and ERROR alerts only."
            }}
          </p>
        </div>
        <div class="actions">
          <button
            class="btn"
            :class="{ primary: status.debugMode }"
            type="button"
            title="When OFF, only INFO/WARN/ERROR alerts are recorded. Turn ON to record every TRACE/DEBUG span and pipeline waterfall."
            @click="
              status.debugMode
                ? setDebugMode(false, status.debugRealTrans)
                : setDebugMode(true, true)
            "
          >
            {{ status.debugMode ? "Verbose Trace: ON" : "Verbose Trace: OFF" }}
          </button>
          <label
            class="chk"
            title="Replaces live translations with 'نص اختباري <id>' (0 ms) to benchmark hook & Arabic shaping without network latency."
          >
            <input
              type="checkbox"
              :checked="status.debugMode && !status.debugRealTrans"
              :disabled="!status.debugMode"
              @change="setDebugMode(status.debugMode, !$event.target.checked)"
            />
            <span>Mock Translation (0 ms)</span>
          </label>
          <button class="btn" type="button" @click="handleExport">
            Export .jsonl
          </button>
          <button class="btn" type="button" @click="handleClearAll">
            Clear
          </button>
        </div>
      </div>

      <div v-if="exportMsg" class="toast mono">{{ exportMsg }}</div>

      <div class="stats">
        <div class="stat">
          <span class="slbl">Total Calls</span>
          <span class="sval">{{ healthSummary.totalCalls }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Cache Hit Ratio</span>
          <span class="sval">{{ healthSummary.hitRatio }}%</span>
        </div>
        <div class="stat">
          <span class="slbl">Avg Embed Latency</span>
          <span class="sval">{{ fmtUs(healthSummary.embedAvgUs) }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Native Panics</span>
          <span class="sval" :class="{ err: healthSummary.panics > 0 }">
            {{ healthSummary.panics }}
          </span>
        </div>
        <div class="stat">
          <span class="slbl">Dropped Events</span>
          <span class="sval" :class="{ warn: healthSummary.dropped > 0 }">
            {{ healthSummary.dropped }}
          </span>
        </div>
      </div>

      <!-- Panel B: Critical Function Watch Table -->
      <div class="panel">
        <div class="panel-head" @click="showMatrix = !showMatrix">
          <span class="panel-title">
            {{ showMatrix ? "▾" : "▸" }} Critical Function Health Matrix
            ({{ functionStats.length }})
          </span>
          <span v-if="selectedFunc" class="filter-chip" @click.stop="selectedFunc = ''">
            Filtered: {{ selectedFunc }} ✕
          </span>
        </div>
        <div v-if="showMatrix" class="table-scroll matrix-scroll">
          <table class="tbl">
            <thead>
              <tr>
                <th></th>
                <th>Subsystem</th>
                <th>Function</th>
                <th>Calls</th>
                <th>Err / TO</th>
                <th>Last</th>
                <th>Avg</th>
                <th>Max</th>
                <th>Last Error / Time</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!functionStats.length">
                <td colspan="9" class="empty">
                  No function spans recorded yet. Attach to a game or trigger translation.
                </td>
              </tr>
              <tr
                v-for="st in functionStats"
                :key="st.func"
                class="click-row"
                :class="{ active: selectedFunc === st.func }"
                @click="toggleFuncFilter(st.func)"
              >
                <td><span class="dot" :class="statHealthClass(st)"></span></td>
                <td><span class="sub-badge">{{ st.subsystem }}</span></td>
                <td class="mono">{{ st.func }}</td>
                <td class="mono">{{ st.calls }}</td>
                <td class="mono" :class="{ err: st.errors > 0 || st.timeouts > 0 }">
                  {{ st.errors }} / {{ st.timeouts }}
                </td>
                <td class="mono">{{ fmtUs(st.lastDurUs) }}</td>
                <td class="mono">{{ fmtUs(st.avgDurUs) }}</td>
                <td class="mono">{{ fmtUs(st.maxDurUs) }}</td>
                <td class="mono note">
                  {{ st.lastError || st.lastCalledAt }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Panel C: Unified Live Stream & Pipeline Waterfall -->
      <div class="panel stream-panel">
        <div class="panel-head" @click="showStream = !showStream">
          <span class="panel-title">
            {{ showStream ? "▾" : "▸" }} Live Telemetry Stream ({{ filteredEvents.length }})
          </span>
          <button class="btn sm" type="button" @click.stop="togglePause">
            {{ paused ? "Resume" : "Pause" }}
          </button>
        </div>

        <div v-if="showStream" class="filters">
          <div class="pills">
            <button
              v-for="sub in SUBSYSTEMS"
              :key="sub"
              type="button"
              class="pill"
              :class="{ active: selectedSubsystem === sub }"
              @click="selectedSubsystem = sub"
            >
              {{ sub }}
            </button>
          </div>
          <div class="filter-right">
            <select v-model="selectedLevel" class="sel">
              <option v-for="l in LEVELS" :key="l.id" :value="l.id">
                {{ l.label }}
              </option>
            </select>
            <input
              v-model="searchQuery"
              class="search"
              type="text"
              placeholder="Search func, msg, hook key…"
            />
          </div>
        </div>

        <div v-if="showStream" class="table-scroll stream-scroll">
          <table class="tbl">
            <thead>
              <tr>
                <th>Time</th>
                <th>Lvl</th>
                <th>Subsystem</th>
                <th>Function</th>
                <th>Duration</th>
                <th>Message &amp; Pipeline Waterfall</th>
              </tr>
            </thead>
            <tbody>
              <tr v-if="!filteredEvents.length">
                <td colspan="6" class="empty">
                  No telemetry events match the current filters.
                </td>
              </tr>
              <tr v-for="ev in filteredEvents.slice(0, 300)" :key="ev.seq">
                <td class="mono muted">{{ ev.timestamp }}</td>
                <td>
                  <span class="lvl" :class="'lvl-' + ev.level">{{ ev.level.toUpperCase() }}</span>
                </td>
                <td><span class="sub-badge">{{ ev.subsystem }}</span></td>
                <td class="mono">{{ ev.func }}</td>
                <td class="mono">
                  <span v-if="ev.durationUs" class="dur-badge" :class="durationBadgeClass(ev)">
                    {{ fmtUs(ev.durationUs) }}
                  </span>
                  <span v-else class="muted">—</span>
                </td>
                <td class="msg-cell">
                  <div class="msg-line">
                    <span>{{ ev.msg }}</span>
                    <span v-if="ev.fields?.key" class="mono muted"> [{{ ev.fields.key }}]</span>
                    <span v-if="ev.fields?.tier" class="mono muted"> ({{ ev.fields.tier }})</span>
                  </div>
                  <div v-if="waterfallSegments(ev)" class="wf-bar" title="Pre -> Trans -> Post -> Shape -> IPC">
                    <span
                      v-for="seg in waterfallSegments(ev)"
                      :key="seg.name"
                      class="wf-seg"
                      :class="seg.cls"
                      :style="{ width: seg.w + '%' }"
                      :title="`${seg.name}: ${seg.us} µs`"
                    ></span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
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
  min-height: 0;
}
.card {
  max-width: 1180px;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  min-height: 0;
}
.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.h2 {
  margin: 0 0 2px;
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
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  cursor: pointer;
}
.btn {
  height: 28px;
  padding: 0 11px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.btn.sm {
  height: 22px;
  padding: 0 8px;
  font-size: 11px;
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
.toast {
  padding: 6px 10px;
  background: rgba(34, 197, 94, 0.14);
  border: 1px solid rgba(34, 197, 94, 0.35);
  border-radius: 5px;
  font-size: 11px;
}
.stats {
  display: grid;
  grid-template-columns: repeat(5, 1fr);
  gap: 8px;
}
.stat {
  padding: 7px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.slbl {
  font-size: 11px;
  color: var(--fg-muted);
}
.sval {
  font-family: var(--mono);
  font-size: 13px;
  font-weight: 600;
}
.sval.err {
  color: #ef4444;
}
.sval.warn {
  color: #f59e0b;
}
.panel {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-height: 0;
}
.stream-panel {
  flex: 1;
}
.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  cursor: pointer;
  user-select: none;
  padding: 2px 0;
}
.panel-title {
  font-size: 12px;
  font-weight: 600;
}
.filter-chip {
  font-size: 11px;
  font-family: var(--mono);
  padding: 2px 7px;
  border-radius: 4px;
  background: var(--accent);
  color: #fff;
}
.filters {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  flex-wrap: wrap;
}
.pills {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}
.pill {
  padding: 3px 8px;
  font-size: 11px;
  border-radius: 4px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg-muted);
  cursor: pointer;
}
.pill.active {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.filter-right {
  display: flex;
  gap: 6px;
  align-items: center;
}
.sel,
.search {
  height: 26px;
  padding: 0 8px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 11px;
}
.search {
  width: 200px;
}
.table-scroll {
  overflow: auto;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
  min-height: 0;
}
.matrix-scroll {
  max-height: 185px;
}
.stream-scroll {
  flex: 1;
}
.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 11px;
}
.tbl th {
  position: sticky;
  top: 0;
  background: var(--bg-elev);
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid var(--border);
  color: var(--fg-muted);
  font-weight: 600;
  z-index: 1;
}
.tbl td {
  padding: 5px 8px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.click-row {
  cursor: pointer;
}
.click-row:hover,
.click-row.active {
  background: rgba(59, 130, 246, 0.08);
}
.mono {
  font-family: var(--mono);
}
.muted {
  color: var(--fg-muted);
}
.note {
  max-width: 260px;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--fg-muted);
}
.err {
  color: #ef4444;
  font-weight: 600;
}
.dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.dot-green {
  background: #22c55e;
}
.dot-amber {
  background: #f59e0b;
}
.dot-red {
  background: #ef4444;
}
.sub-badge {
  font-family: var(--mono);
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 3px;
  border: 1px solid var(--border);
  background: var(--bg-elev);
}
.lvl {
  font-family: var(--mono);
  font-size: 10px;
  font-weight: 600;
}
.lvl-error {
  color: #ef4444;
}
.lvl-warn {
  color: #f59e0b;
}
.lvl-info {
  color: #3b82f6;
}
.lvl-debug,
.lvl-trace {
  color: var(--fg-muted);
}
.dur-badge {
  padding: 1px 5px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
}
.badge-green {
  background: rgba(34, 197, 94, 0.16);
  color: #22c55e;
}
.badge-blue {
  background: rgba(59, 130, 246, 0.16);
  color: #60a5fa;
}
.badge-amber {
  background: rgba(245, 158, 11, 0.18);
  color: #f59e0b;
}
.badge-red {
  background: rgba(239, 68, 68, 0.18);
  color: #ef4444;
}
.msg-cell {
  white-space: normal !important;
  min-width: 240px;
}
.msg-line {
  word-break: break-word;
}
.wf-bar {
  display: flex;
  height: 4px;
  width: 100%;
  max-width: 320px;
  margin-top: 3px;
  border-radius: 2px;
  overflow: hidden;
  background: var(--bg-elev);
}
.wf-seg {
  height: 100%;
}
.wf-pre {
  background: #8b5cf6;
}
.wf-trans {
  background: #3b82f6;
}
.wf-post {
  background: #06b6d4;
}
.wf-shape {
  background: #22c55e;
}
.wf-cb {
  background: #f59e0b;
}
.empty {
  text-align: center;
  color: var(--fg-muted);
  padding: 20px 10px !important;
}
</style>
