import { ref } from "vue";
import { callApp, onEvent } from "./wailsBridge.js";
import { clearError, setError } from "./sessionState.js";

export const logs = ref([]);
export const debugTraces = ref([]);
export const telemetryEvents = ref([]);
export const functionStats = ref([]);
export const droppedEvents = ref(0);

export function registerTelemetryListeners({ enqueueLog, enqueueTrace }) {
  onEvent("log", enqueueLog);
  onEvent("debug:trace", enqueueTrace);

  onEvent("telemetry:batch", (batch) => {
    if (!batch) return;
    if (Array.isArray(batch.events) && batch.events.length) {
      const incoming = [...batch.events].reverse();
      telemetryEvents.value = [...incoming, ...telemetryEvents.value].slice(0, 2000);
    }
    if (Array.isArray(batch.stats)) {
      functionStats.value = batch.stats;
    }
    if (typeof batch.droppedEvents === "number") {
      droppedEvents.value = batch.droppedEvents;
    }
  });
}

export function setDebugMode(enabled, realTrans = true) {
  return callApp("SetDebugMode", Boolean(enabled), Boolean(realTrans));
}

export function clearDebugTraces() {
  debugTraces.value = [];
}

export async function refreshTelemetry() {
  try {
    const snap = await callApp("GetTelemetrySnapshot");
    if (!snap) return;
    if (Array.isArray(snap.events)) {
      telemetryEvents.value = [...snap.events].reverse().slice(0, 2000);
    }
    if (Array.isArray(snap.stats)) {
      functionStats.value = snap.stats;
    }
    if (typeof snap.droppedEvents === "number") {
      droppedEvents.value = snap.droppedEvents;
    }
  } catch (e) {
    setError(e);
  }
}

export async function clearTelemetry() {
  clearError();
  try {
    await callApp("ClearTelemetry");
    telemetryEvents.value = [];
    functionStats.value = [];
    droppedEvents.value = 0;
  } catch (e) {
    setError(e);
  }
}

export async function exportDiagnosticBundle() {
  clearError();
  try {
    return await callApp("ExportDiagnosticBundle");
  } catch (e) {
    setError(e);
    return "";
  }
}
