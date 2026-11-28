import { ref, shallowRef } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, setError } from "./sessionState.js";

export const benchmarkItems = ref([]);
export const benchmarkRunning = shallowRef(false);
export const benchmarkRunId = shallowRef("");
export const benchmarkTotal = shallowRef(0);

export function registerBenchmarkListeners() {
  onEvent("translator:bench:item", (item) => {
    if (!item || item.runId !== benchmarkRunId.value) return;
    const idx = benchmarkItems.value.findIndex((x) => x.providerId === item.providerId);
    if (idx >= 0) {
      benchmarkItems.value[idx] = item;
    } else {
      benchmarkItems.value = [...benchmarkItems.value, item];
    }
  });

  onEvent("translator:bench:done", (summary) => {
    if (!summary || summary.runId !== benchmarkRunId.value) return;
    if (Array.isArray(summary.items)) {
      benchmarkItems.value = summary.items;
    }
    benchmarkTotal.value = summary.total || benchmarkTotal.value;
    benchmarkRunning.value = false;
  });
}

export async function benchmarkTranslators(req) {
  clearError();
  const runId = `bench-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  benchmarkRunId.value = runId;
  benchmarkItems.value = [];
  const total =
    Array.isArray(req.providers) && req.providers.length
      ? req.providers.length
      : (req.includeTraditional ? 13 : 0) + (req.includeLlm ? 1 : 0) + (req.includeOfflineLlm ? 1 : 0);
  benchmarkTotal.value = total;
  benchmarkRunning.value = true;
  try {
    const sum = await callApp("BenchmarkTranslators", { ...req, runId });
    if (sum && sum.runId === benchmarkRunId.value && Array.isArray(sum.items)) {
      benchmarkItems.value = sum.items;
      benchmarkTotal.value = sum.total || total;
    }
    return sum;
  } catch (e) {
    setError(e);
    return null;
  } finally {
    if (benchmarkRunId.value === runId) {
      benchmarkRunning.value = false;
    }
  }
}

export async function cancelBenchmark() {
  await callApp("CancelBenchmark");
  benchmarkRunning.value = false;
}
