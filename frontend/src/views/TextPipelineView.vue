<script setup>
import { ref, watch } from "vue";
import Icon from "../components/Icon.vue";
import { useSession } from "../composables/useSession.js";

const { config, saveConfig, saveState } = useSession();

const preRules = ref([]);
const postRules = ref([]);

watch(
  config,
  (cfg) => {
    if (!cfg) return;
    preRules.value = (cfg.pipeline?.preRules || []).map((r) => ({ ...r }));
    postRules.value = (cfg.pipeline?.postRules || []).map((r) => ({ ...r }));
  },
  { immediate: true },
);

function addRule(list) {
  list.push({ enabled: true, regex: false, pattern: "", replace: "" });
}

function removeRule(list, idx) {
  list.splice(idx, 1);
}

function onSave() {
  if (!config.value) return;
  saveConfig({
    ...config.value,
    pipeline: {
      preRules: preRules.value,
      postRules: postRules.value,
    },
  });
}
</script>

<template>
  <div class="page">
    <div class="card">
      <div class="head">
        <div>
          <h2 class="h2">Pre-Translation Rules</h2>
          <p class="sub">Strip noise or replace terms before sending game text to the translator.</p>
        </div>
        <button class="btn" type="button" @click="addRule(preRules)">+ Add Rule</button>
      </div>

      <div v-if="!preRules.length" class="empty">No pre-translation rules configured.</div>
      <div v-for="(r, idx) in preRules" :key="'pre-' + idx" class="rule-row">
        <input v-model="r.enabled" type="checkbox" />
        <label class="regex-chk">
          <input v-model="r.regex" type="checkbox" />
          <span>Regex</span>
        </label>
        <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
        <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
        <button class="icon-btn" type="button" @click="removeRule(preRules, idx)">
          <Icon name="Trash" :size="13" />
        </button>
      </div>
    </div>

    <div class="card">
      <div class="head">
        <div>
          <h2 class="h2">Post-Translation Correction (Before Arabic Shaping)</h2>
          <p class="sub">Fix recurring translation quirks before logical wrapping &amp; BiDi shaping.</p>
        </div>
        <button class="btn" type="button" @click="addRule(postRules)">+ Add Rule</button>
      </div>

      <div v-if="!postRules.length" class="empty">No post-translation rules configured.</div>
      <div v-for="(r, idx) in postRules" :key="'post-' + idx" class="rule-row">
        <input v-model="r.enabled" type="checkbox" />
        <label class="regex-chk">
          <input v-model="r.regex" type="checkbox" />
          <span>Regex</span>
        </label>
        <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
        <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
        <button class="icon-btn" type="button" @click="removeRule(postRules, idx)">
          <Icon name="Trash" :size="13" />
        </button>
      </div>

      <div class="actions">
        <button class="btn primary" type="button" @click="onSave">
          {{ saveState === "saved" ? "Saved ✓" : "Save Pipeline Rules" }}
        </button>
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
  gap: 14px;
}
.card {
  max-width: 820px;
  padding: 14px 18px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
}
.sub {
  margin: 2px 0 0;
  font-size: 11px;
  color: var(--fg-muted);
}
.rule-row {
  display: grid;
  grid-template-columns: auto auto 1fr 1fr auto;
  align-items: center;
  gap: 8px;
}
.regex-chk {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--fg-muted);
}
.input {
  height: 28px;
  padding: 0 8px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.mono {
  font-family: var(--mono);
}
.btn {
  height: 28px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.icon-btn {
  height: 28px;
  width: 28px;
  background: transparent;
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 5px;
  cursor: pointer;
  display: grid;
  place-items: center;
}
.empty {
  color: var(--fg-muted);
  font-size: 12px;
  padding: 8px 0;
}
.actions {
  display: flex;
  justify-content: flex-end;
}
</style>
