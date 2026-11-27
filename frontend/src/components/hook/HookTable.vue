<script setup>
import { shallowRef, computed, watch } from "vue";
import Icon from "../Icon.vue";
import Tooltip from "../Tooltip.vue";
import { useI18n } from "../../composables/useI18n.js";
import { useSession } from "../../composables/useSession.js";
import { formatHookLabel } from "../../utils/hookLabels.js";
import { usePinnedHookSort } from "../../composables/usePinnedHookSort.js";

const props = defineProps({
  hooks: { type: Array, required: true },
  outputs: { type: Object, required: true },
  active: { type: Boolean, default: false },
});

const emit = defineEmits(["toggle-embed"]);
const { t, lang } = useI18n();
const { config, saveConfig } = useSession();
const { snapshotPinned, sortPinned, needsSort } = usePinnedHookSort(() => props.hooks);

async function toggleVerboseHooks(val) {
  if (!config.value) return;
  await saveConfig({
    ...config.value,
    embed: { ...config.value.embed, verboseHooks: Boolean(val) },
  });
}

watch(
  () => props.active,
  (isActive) => {
    if (isActive) snapshotPinned();
  },
);

const query = shallowRef("");
const embedOnly = shallowRef(true);

const filteredHooks = computed(() => {
  const q = query.value.trim().toLowerCase();
  const filtered = props.hooks.filter((h) => {
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
  return sortPinned(filtered);
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
          :placeholder="t('htFilterPlaceholder')"
        />
      </div>
      <label class="chk">
        <input v-model="embedOnly" type="checkbox" />
        <span>{{ t("htEmbedOnly", { shown: filteredHooks.length, total: props.hooks.length }) }}</span>
      </label>
      <label class="chk" :title="t('verboseHooksTip')">
        <input
          type="checkbox"
          :checked="Boolean(config?.embed?.verboseHooks)"
          @change="toggleVerboseHooks($event.target.checked)"
        />
        <span>{{ t("verboseHooksLabel") }}</span>
      </label>
      <Tooltip :text="t('sortCheckedTopTip')">
        <button
          class="sort-btn"
          :class="{ dirty: needsSort }"
          type="button"
          :disabled="!filteredHooks.length"
          @click="snapshotPinned"
        >
          <Icon name="ArrowUpDown" :size="12" />
          <span>{{ t("sortCheckedTop") }}</span>
        </button>
      </Tooltip>
    </div>

    <div class="scroll">
      <table class="tbl">
        <thead>
          <tr>
            <th class="col-embed">{{ t("htColEmbed") }}</th>
            <th class="col-name">{{ t("htColName") }}</th>
            <th>{{ t("htColText") }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!filteredHooks.length">
            <td colspan="3" class="empty">{{ t("htEmpty") }}</td>
          </tr>
          <tr
            v-for="(h, idx) in filteredHooks"
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
                  <span class="name">{{ formatHookLabel(h, idx, lang) }}</span>
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
.sort-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
}
.sort-btn.dirty {
  border-color: var(--accent);
  color: var(--accent);
}
.sort-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
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
