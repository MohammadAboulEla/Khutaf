<script setup>
import { shallowRef, computed } from "vue";
import Icon from "../Icon.vue";

const props = defineProps({
  status: { type: Object, required: true },
  processes: { type: Array, required: true },
});

const emit = defineEmits(["attach", "detach", "refresh", "insert-code"]);

const selectedPid = shallowRef("");
const hookCode = shallowRef("");

const canAttach = computed(() => Boolean(selectedPid.value));

function onAttach() {
  if (selectedPid.value) emit("attach", Number(selectedPid.value));
}

function onInsert() {
  if (!hookCode.value.trim()) return;
  emit("insert-code", hookCode.value.trim());
  hookCode.value = "";
}
</script>

<template>
  <section class="panel">
    <div class="row">
      <select v-model="selectedPid" class="select">
        <option value="">Select running game process…</option>
        <option v-for="p in props.processes" :key="p.pid" :value="p.pid">
          [{{ p.pid }}] {{ p.name }} — {{ p.title }}
          {{ p.engine ? `(${p.engine})` : "" }}
        </option>
      </select>

      <button class="btn" type="button" @click="emit('refresh')">
        <Icon name="RefreshCw" :size="13" />
        <span>Refresh</span>
      </button>

      <button
        v-if="!props.status.attached"
        class="btn primary"
        type="button"
        :disabled="!canAttach"
        @click="onAttach"
      >
        <Icon name="Play" :size="13" />
        <span>Attach</span>
      </button>
      <button v-else class="btn danger" type="button" @click="emit('detach')">
        <Icon name="Stop" :size="13" />
        <span>Detach</span>
      </button>
    </div>

    <div v-if="props.status.attached" class="row meta-row">
      <span class="badge ok">Attached</span>
      <span class="mono">{{ props.status.gameName }}</span>
      <span v-if="props.status.engine" class="badge">{{ props.status.engine }}</span>
      <span class="badge">{{ props.status.is64 ? "x64" : "x86" }}</span>
      <span class="muted">PIDs: {{ props.status.pids?.join(", ") }}</span>

      <div class="spacer" />

      <input
        v-model="hookCode"
        class="input code-input"
        placeholder="Insert H-code (e.g. /HS-4@1234:game.exe)"
        @keyup.enter="onInsert"
      />
      <button class="btn" type="button" @click="onInsert">Insert Hook</button>
    </div>
  </section>
</template>

<style scoped>
.panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
}
.row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.meta-row {
  padding-top: 6px;
  border-top: 1px solid var(--border);
  font-size: 12px;
}
.select,
.input {
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.select {
  flex: 1;
  min-width: 240px;
  font-family: var(--mono);
}
.code-input {
  width: 260px;
  font-family: var(--mono);
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
  cursor: pointer;
}
.btn:hover:not(:disabled) {
  background: color-mix(in oklab, var(--fg) 8%, var(--paper));
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
.badge {
  padding: 2px 7px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  font-family: var(--mono);
  font-size: 11px;
}
.badge.ok {
  background: color-mix(in oklab, #16a34a 22%, transparent);
  color: #4ade80;
}
.mono {
  font-family: var(--mono);
  font-weight: 600;
}
.muted {
  color: var(--fg-muted);
  font-family: var(--mono);
}
.spacer {
  flex: 1;
}
</style>
