<script setup>
import { computed, onMounted, ref, shallowRef, watch } from "vue";
import Icon from "../components/Icon.vue";
import { useSession } from "../composables/useSession.js";

const {
  config,
  savedGames,
  pipelineTarget,
  refreshSavedGames,
  saveConfig,
  saveGamePipeline,
  saveState,
} = useSession();

const preRules = ref([]);
const postRules = ref([]);
const preSample = shallowRef("<b>Guide:</b> Welcome<br>to Wonderia!");
const postSample = shallowRef("الدليل: مرحباً بك في وونديريا!");

onMounted(() => {
  refreshSavedGames();
});

function loadRules() {
  if (pipelineTarget.value === "global") {
    const p = config.value?.pipeline;
    preRules.value = (p?.preRules || []).map((r) => ({ ...r }));
    postRules.value = (p?.postRules || []).map((r) => ({ ...r }));
    return;
  }
  const game = savedGames.value.find((g) => g.exePath === pipelineTarget.value);
  const p = game?.pipeline;
  preRules.value = (p?.preRules || []).map((r) => ({ ...r }));
  postRules.value = (p?.postRules || []).map((r) => ({ ...r }));
}

watch([config, savedGames, pipelineTarget], loadRules, { immediate: true });

function addRule(list) {
  list.push({ enabled: true, regex: false, pattern: "", replace: "" });
}

function removeRule(list, idx) {
  list.splice(idx, 1);
}

function onSave() {
  const payload = {
    preRules: preRules.value,
    postRules: postRules.value,
  };
  if (pipelineTarget.value === "global") {
    if (!config.value) return;
    saveConfig({ ...config.value, pipeline: payload });
  } else {
    saveGamePipeline(pipelineTarget.value, payload);
  }
}

function applyRulesPreview(text, rules) {
  let out = text;
  let error = "";
  for (const r of rules) {
    if (!r.enabled || !r.pattern) continue;
    if (r.regex) {
      try {
        out = out.replace(new RegExp(r.pattern, "g"), r.replace || "");
      } catch (e) {
        error = String(e.message || e);
      }
    } else {
      out = out.split(r.pattern).join(r.replace || "");
    }
  }
  return { out, error };
}

const preview = computed(() => {
  const pre = applyRulesPreview(preSample.value, preRules.value);
  const post = applyRulesPreview(postSample.value, postRules.value);
  return {
    afterPre: pre.out,
    afterPost: post.out,
    error: pre.error || post.error,
  };
});
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h2 class="h2">Text Pipeline Rules</h2>
        <p class="sub">Global rules apply to every game; per-game rules run on top of global rules only for that game.</p>
      </div>
      <div class="toolbar-actions">
        <select v-model="pipelineTarget" class="scope-select">
          <option value="global">Global (All Games)</option>
          <option v-for="g in savedGames" :key="g.exePath" :value="g.exePath">
            {{ g.title }}{{ g.attached ? " (Attached)" : "" }}
          </option>
        </select>
        <button class="btn primary" type="button" @click="onSave">
          {{ saveState === "saving" ? "Saving…" : saveState === "saved" ? "Saved ✓" : "Save Rules" }}
        </button>
      </div>
    </div>

    <div class="sections">
      <div class="card">
        <div class="head">
          <div class="head-title">
            <span class="step">Step 1</span>
            <div>
              <h3 class="h3">Pre-Translation Rules</h3>
              <p class="sub">Strip noise or normalize text before sending it to the translator.</p>
            </div>
          </div>
          <button class="btn" type="button" @click="addRule(preRules)">+ Add Rule</button>
        </div>

        <div v-if="!preRules.length" class="empty">No pre-translation rules configured for this scope.</div>
        <div v-for="(r, idx) in preRules" :key="'pre-' + idx" class="rule-row">
          <input v-model="r.enabled" type="checkbox" title="Enable rule" />
          <button
            type="button"
            class="mode-chip"
            :class="{ active: r.regex }"
            @click="r.regex = !r.regex"
          >
            {{ r.regex ? "Regex" : "Literal" }}
          </button>
          <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
          <span class="arrow">→</span>
          <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
          <button class="icon-btn" type="button" @click="removeRule(preRules, idx)">
            <Icon name="Trash" :size="13" />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="head">
          <div class="head-title">
            <span class="step">Step 2</span>
            <div>
              <h3 class="h3">Post-Translation Correction</h3>
              <p class="sub">Fix recurring translation quirks before Arabic shaping &amp; wrapping.</p>
            </div>
          </div>
          <button class="btn" type="button" @click="addRule(postRules)">+ Add Rule</button>
        </div>

        <div v-if="!postRules.length" class="empty">No post-translation rules configured for this scope.</div>
        <div v-for="(r, idx) in postRules" :key="'post-' + idx" class="rule-row">
          <input v-model="r.enabled" type="checkbox" title="Enable rule" />
          <button
            type="button"
            class="mode-chip"
            :class="{ active: r.regex }"
            @click="r.regex = !r.regex"
          >
            {{ r.regex ? "Regex" : "Literal" }}
          </button>
          <input v-model="r.pattern" class="input mono" placeholder="Pattern to match…" />
          <span class="arrow">→</span>
          <input v-model="r.replace" class="input mono" placeholder="Replacement…" />
          <button class="icon-btn" type="button" @click="removeRule(postRules, idx)">
            <Icon name="Trash" :size="13" />
          </button>
        </div>
      </div>

      <div class="card">
        <div class="head">
          <div>
            <h3 class="h3">Live Rule Tester</h3>
            <p class="sub">Test Pre-Rules on raw game text (left) and Post-Rules on translated Arabic (right).</p>
          </div>
          <span v-if="preview.error" class="err">{{ preview.error }}</span>
        </div>
        <div class="preview-grid">
          <div class="pbox">
            <div class="plbl">Raw Game Text → After Pre-Rules</div>
            <input v-model="preSample" class="input mono pinput" dir="auto" placeholder="Raw game text (e.g. <b>Guide:</b> Hello<br>world)…" />
            <div class="pval mono" dir="auto">{{ preview.afterPre }}</div>
          </div>
          <div class="pbox">
            <div class="plbl">Translated Arabic → After Post-Rules</div>
            <input v-model="postSample" class="input pinput" dir="rtl" placeholder="Translated Arabic text…" />
            <div class="pval" dir="rtl">{{ preview.afterPost }}</div>
          </div>
        </div>
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
  gap: 12px;
}
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding: 12px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.scope-select {
  height: 30px;
  padding: 0 10px;
  min-width: 240px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
}
.sections {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.card {
  padding: 14px 16px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.head-title {
  display: flex;
  align-items: center;
  gap: 10px;
}
.step {
  padding: 2px 8px;
  border-radius: 4px;
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  color: var(--accent);
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
  flex: none;
}
.h2 {
  margin: 0 0 3px;
  font-size: 14px;
  font-weight: 600;
}
.h3 {
  margin: 0 0 2px;
  font-size: 13.5px;
  font-weight: 600;
}
.sub {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.rule-row {
  display: grid;
  grid-template-columns: auto auto 1fr auto 1fr auto;
  gap: 8px;
  align-items: center;
}
.mode-chip {
  height: 28px;
  min-width: 58px;
  padding: 0 8px;
  background: var(--paper);
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-family: var(--mono);
  font-size: 11px;
  cursor: pointer;
}
.mode-chip.active {
  background: color-mix(in oklab, var(--accent) 18%, transparent);
  border-color: var(--accent);
  color: var(--accent);
  font-weight: 600;
}
.arrow {
  color: var(--fg-muted);
  font-family: var(--mono);
  font-size: 12px;
}
.input {
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  min-width: 0;
}
.mono {
  font-family: var(--mono);
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
.btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.icon-btn {
  height: 30px;
  width: 30px;
  background: var(--paper);
  color: var(--fg-muted);
  border: 1px solid var(--border);
  border-radius: 5px;
  cursor: pointer;
  display: grid;
  place-items: center;
}
.icon-btn:hover {
  color: #f87171;
}
.empty {
  color: var(--fg-muted);
  font-size: 12px;
  padding: 4px 0;
}
.preview-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}
.pbox {
  padding: 8px 10px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.pinput {
  width: 100%;
  background: var(--bg-elev);
}
.plbl {
  font-size: 11px;
  color: var(--fg-muted);
  margin-bottom: 3px;
}
.pval {
  font-size: 12px;
  word-break: break-word;
}
.err {
  color: #f87171;
  font-family: var(--mono);
  font-size: 11px;
}
</style>
