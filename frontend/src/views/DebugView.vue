<script setup>
import { computed, shallowRef } from "vue";
import { useSession } from "../composables/useSession.js";

const { status, debugTraces, setDebugMode, clearDebugTraces } = useSession();

const copied = shallowRef(false);

const stats = computed(() => {
  const list = debugTraces.value;
  if (!list.length) {
    return {
      count: 0,
      avgTotal: 0,
      maxTotal: 0,
      avgShape: 0,
      avgCallback: 0,
      avgRules: 0,
    };
  }
  let sumTotal = 0;
  let maxTotal = 0;
  let sumShape = 0;
  let sumCb = 0;
  let sumRules = 0;
  for (const t of list) {
    sumTotal += t.totalUs;
    if (t.totalUs > maxTotal) maxTotal = t.totalUs;
    sumShape += t.shapeUs;
    sumCb += t.callbackUs;
    sumRules += t.preRulesUs + t.postRulesUs;
  }
  const n = list.length;
  return {
    count: n,
    avgTotal: Math.round(sumTotal / n),
    maxTotal,
    avgShape: Math.round(sumShape / n),
    avgCallback: Math.round(sumCb / n),
    avgRules: Math.round(sumRules / n),
  };
});

function fmtUs(us) {
  if (us >= 1000) return `${(us / 1000).toFixed(2)} ms`;
  return `${us} µs`;
}

async function copyReport() {
  const st = status.value;
  const s = stats.value;
  const lines = [
    "# Khutaf Performance Debug Report",
    `Game: ${st.gameName || "not attached"} (${st.engine || "unknown"}, ${st.is64 ? "x64" : "x86"})`,
    `Samples: ${s.count} | Avg Total: ${fmtUs(s.avgTotal)} | Max Total: ${fmtUs(s.maxTotal)} | Avg Shape: ${fmtUs(s.avgShape)} | Avg Callback: ${fmtUs(s.avgCallback)} | Avg Rules: ${fmtUs(s.avgRules)}`,
    "",
    "| # | Time | Chars | Pre | Trans | Post | Shape | Callback | Total | Hook Key |",
    "|---|------|-------|-----|-------|------|-------|----------|-------|----------|",
  ];
  for (const t of debugTraces.value.slice(0, 100)) {
    lines.push(
      `| ${t.seq} | ${t.time} | ${t.origLen} | ${t.preRulesUs}µs | ${t.translateUs}µs | ${t.postRulesUs}µs | ${t.shapeUs}µs | ${t.callbackUs}µs | ${fmtUs(t.totalUs)} | ${t.key} |`,
    );
  }
  await navigator.clipboard.writeText(lines.join("\n"));
  copied.value = true;
  setTimeout(() => {
    copied.value = false;
  }, 1500);
}
</script>

<template>
  <div class="page">
    <div class="card">
      <div class="head">
        <div>
          <h2 class="h2">Performance Debug &amp; Profiling</h2>
          <p class="sub">
            Disables real translation requests (0 ms) and injects
            <code>نص اختباري &lt;id&gt;</code> to measure hook, rules, Arabic
            shaping, and callback timings.
          </p>
        </div>
        <div class="actions">
          <button
            class="btn"
            :class="{ primary: !status.debugMode, danger: status.debugMode }"
            type="button"
            @click="setDebugMode(!status.debugMode, status.debugRealTrans)"
          >
            {{ status.debugMode ? "Stop Debugging" : "Start Debugging" }}
          </button>
          <button
            class="btn"
            type="button"
            :disabled="!debugTraces.length"
            @click="copyReport"
          >
            {{ copied ? "Copied ✓" : "Copy Report" }}
          </button>
          <button
            class="btn"
            type="button"
            :disabled="!debugTraces.length"
            @click="clearDebugTraces"
          >
            Clear
          </button>
          <label class="chk">
            <input
              type="checkbox"
              :checked="status.debugRealTrans"
              @change="setDebugMode(status.debugMode, $event.target.checked)"
            />
            <span>Test with real translation</span>
          </label>
        </div>
      </div>

      <div class="stats">
        <div class="stat">
          <span class="slbl">Samples</span>
          <span class="sval">{{ stats.count }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Avg Total</span>
          <span class="sval">{{ fmtUs(stats.avgTotal) }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Max Total</span>
          <span class="sval">{{ fmtUs(stats.maxTotal) }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Avg Rules</span>
          <span class="sval">{{ fmtUs(stats.avgRules) }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Avg Shape</span>
          <span class="sval">{{ fmtUs(stats.avgShape) }}</span>
        </div>
        <div class="stat">
          <span class="slbl">Avg Callback</span>
          <span class="sval">{{ fmtUs(stats.avgCallback) }}</span>
        </div>
      </div>

      <div class="table-scroll">
        <table class="tbl">
          <thead>
            <tr>
              <th>#</th>
              <th>Time</th>
              <th>Chars</th>
              <th>Pre</th>
              <th>Trans</th>
              <th>Post</th>
              <th>Shape</th>
              <th>Callback</th>
              <th>Total</th>
              <th>Hook Key</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!debugTraces.length">
              <td colspan="10" class="empty">
                {{
                  status.debugMode
                    ? "Waiting for game dialogue…"
                    : "Enable debugging and trigger dialogue in the game to record timings."
                }}
              </td>
            </tr>
            <tr v-for="t in debugTraces" :key="t.seq">
              <td class="mono">{{ t.seq }}</td>
              <td class="mono">{{ t.time }}</td>
              <td class="mono">{{ t.origLen }}</td>
              <td class="mono">{{ t.preRulesUs }} µs</td>
              <td class="mono">{{ t.translateUs }} µs</td>
              <td class="mono">{{ t.postRulesUs }} µs</td>
              <td class="mono">{{ t.shapeUs }} µs</td>
              <td class="mono">{{ t.callbackUs }} µs</td>
              <td class="mono total">{{ fmtUs(t.totalUs) }}</td>
              <td class="mono key">{{ t.key }}</td>
            </tr>
          </tbody>
        </table>
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
  max-width: 1040px;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 16px;
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
  margin: 0 0 4px;
  font-size: 14px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.sub code {
  font-family: var(--mono);
  color: var(--fg);
}
.actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  cursor: pointer;
  margin-right: 4px;
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
.btn:disabled {
  opacity: 0.5;
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
.stats {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 8px;
}
.stat {
  padding: 8px 10px;
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
.table-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.tbl {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.tbl th {
  position: sticky;
  top: 0;
  background: var(--bg-elev);
  text-align: left;
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
  color: var(--fg-muted);
  font-size: 11px;
  font-weight: 600;
}
.tbl td {
  padding: 6px 10px;
  border-bottom: 1px solid var(--border);
  white-space: nowrap;
}
.mono {
  font-family: var(--mono);
}
.total {
  font-weight: 600;
}
.key {
  color: var(--fg-muted);
}
.empty {
  text-align: center;
  color: var(--fg-muted);
  padding: 24px 10px !important;
}
</style>
