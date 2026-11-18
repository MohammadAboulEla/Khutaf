<script setup>
import { shallowRef, computed } from "vue";
import Icon from "../Icon.vue";
import Tooltip from "../Tooltip.vue";

const props = defineProps({
  hooks: { type: Array, required: true },
  outputs: { type: Object, required: true },
});

const emit = defineEmits(["toggle-embed"]);

const query = shallowRef("");
const embedOnly = shallowRef(true);

const filteredHooks = computed(() => {
  const q = query.value.trim().toLowerCase();
  return props.hooks.filter((h) => {
    if (embedOnly.value && !h.embeddable) return false;
    if (!q) return true;
    const sample = (props.outputs[h.key] || "").toLowerCase();
    const ctxHex = h.ctx ? h.ctx.toString(16).toLowerCase() : "";
    return (
      h.hookName.toLowerCase().includes(q) ||
      h.hookCode.toLowerCase().includes(q) ||
      ctxHex.includes(q) ||
      sample.includes(q)
    );
  });
});
</script>

<template>
  <section class="table-wrap">
    <div class="toolbar">
      <div class="search-box">
        <Icon name="Search" :size="13" />
        <input
          v-model="query"
          class="search-input"
          placeholder="Filter hooks by name, H-code, or captured text…"
        />
      </div>
      <label class="chk">
        <input v-model="embedOnly" type="checkbox" />
        <span
          >Embeddable only ({{ filteredHooks.length }}/{{
            props.hooks.length
          }})</span
        >
      </label>
    </div>

    <div class="scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th class="col-embed">Embed</th>
            <th class="col-name">Engine / Hook</th>
            <th>Latest Captured Text</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!filteredHooks.length">
            <td colspan="3" class="empty">
              No text hooks discovered yet. Attach to a running game and advance
              dialogue.
            </td>
          </tr>
          <tr
            v-for="h in filteredHooks"
            :key="h.key"
            :class="{ active: h.embedding }"
          >
            <td class="col-embed">
              <input
                type="checkbox"
                :checked="h.embedding"
                :disabled="!h.embeddable"
                @change="emit('toggle-embed', h.key, $event.target.checked)"
              />
            </td>
            <td class="col-name">
              <div class="name-cell">
                <Tooltip
                  :text="`${h.hookName}${h.ctx ? ' #' + h.ctx.toString(16) : ''} (${h.hookCode})`"
                  placement="top"
                  mono
                >
                  <span class="name">{{ h.hookName }}</span>
                </Tooltip>
                <span v-if="h.ctx" class="ctx-tag">#{{ h.ctx.toString(16) }}</span>
                <span v-if="h.embeddable" class="tag">embed</span>
              </div>
            </td>
            <td class="col-text">
              <div class="text-preview">{{ props.outputs[h.key] || "—" }}</div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.table-wrap {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 180px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}
.toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 12px;
  border-bottom: 1px solid var(--border);
}
.search-box {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  height: 28px;
  padding: 0 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--fg-muted);
}
.search-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--fg-muted);
  cursor: pointer;
}
.scroll {
  flex: 1;
  overflow: auto;
}
.tbl {
  width: 100%;
  table-layout: fixed;
  border-collapse: collapse;
  font-size: 12px;
}
.tbl th,
.tbl td {
  padding: 7px 10px;
  border-bottom: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
.tbl th {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--bg-elev);
  color: var(--fg-muted);
  font-weight: 600;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.col-embed {
  width: 56px;
  text-align: center;
}
.col-name {
  width: 240px;
  font-family: var(--mono);
}
.name-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}
.name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.col-code {
  width: 210px;
  font-family: var(--mono);
  color: var(--fg-muted);
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.col-text {
  white-space: pre-wrap;
  word-break: break-word;
}
.text-preview {
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.ctx-tag {
  flex: 0 0 auto;
  padding: 1px 5px;
  border-radius: 3px;
  background: color-mix(in oklab, var(--fg) 10%, transparent);
  color: var(--fg-muted);
  font-size: 10px;
}
.tag {
  flex: 0 0 auto;
  padding: 1px 5px;
  border-radius: 3px;
  background: color-mix(in oklab, var(--accent) 22%, transparent);
  color: var(--accent);
  font-size: 10px;
}
.active {
  background: color-mix(in oklab, var(--accent) 8%, transparent);
}
.empty {
  padding: 28px;
  text-align: center;
  color: var(--fg-muted);
}
</style>
