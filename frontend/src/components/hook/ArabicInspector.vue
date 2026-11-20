<script setup>
import { shallowRef, watch } from "vue";
import { useI18n } from "../../composables/useI18n.js";

const props = defineProps({
  feed: { type: Array, required: true },
  previewFn: { type: Function, required: true },
  translateFn: { type: Function, required: true },
});

const { t } = useI18n();

const tab = shallowRef("feed");
const sampleText = shallowRef("مرحباً يا Ahmed في عالم <color=red>Khutaf</color> نقاط الصحة الخاصة بك <color=#4ade80>100</color> الحالة ممتازة.");
const preview = shallowRef({ logical: "", shaped: "", codes: "" });
const transBusy = shallowRef(false);
const transError = shallowRef("");

watch(
  sampleText,
  async (val) => {
    const res = await props.previewFn(val);
    if (res) preview.value = res;
  },
  { immediate: true },
);

async function onTestTranslate() {
  if (!sampleText.value.trim()) return;
  transBusy.value = true;
  transError.value = "";
  try {
    const res = await props.translateFn(sampleText.value);
    if (res) sampleText.value = res.translated;
  } catch (e) {
    transError.value = String(e);
  } finally {
    transBusy.value = false;
  }
}

// Parse <color=...>...</color> tags into safe renderable segments.
function parseColorSegments(text) {
  if (!text) return [];
  const re = /(<color=([a-zA-Z]+|#[0-9a-fA-F]{3,8})>)(.*?)(<\/color>)/g;
  const out = [];
  let last = 0;
  let m;
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) out.push({ text: text.slice(last, m.index) });
    out.push({ text: m[3], color: m[2] });
    last = re.lastIndex;
  }
  if (last < text.length) out.push({ text: text.slice(last) });
  return out;
}
</script>

<template>
  <section class="pane">
    <div class="pane-head">
      <div class="tabs">
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'feed' }"
          @click="tab = 'feed'"
        >
          {{ t("aiTabFeed", { count: props.feed.length }) }}
        </button>
        <button
          type="button"
          class="tab"
          :class="{ active: tab === 'tester' }"
          @click="tab = 'tester'"
        >
          {{ t("aiTabTester") }}
        </button>
      </div>

      <button
        v-if="tab === 'tester'"
        class="btn"
        type="button"
        :disabled="transBusy"
        @click="onTestTranslate"
      >
        {{ transBusy ? t("trTesting") : t("aiTranslateActive") }}
      </button>
    </div>

    <div v-if="tab === 'feed'" class="feed">
      <div v-if="!props.feed.length" class="empty">
        {{ t("aiFeedEmpty") }}
      </div>
      <div v-for="(item, idx) in props.feed" :key="idx" class="feed-item">
        <div class="feed-meta">
          <span class="mono">{{ item.key }}</span>
          <span>{{ item.elapsedMs }} ms</span>
          <span v-if="item.cached" class="tag">cache</span>
          <span v-if="item.error" class="err-tag">{{ item.error }}</span>
        </div>
        <div class="src">{{ item.original }}</div>
        <div v-if="item.translated" class="ar" dir="rtl">{{ item.translated }}</div>
      </div>
    </div>

    <div v-else class="tester">
      <input
        v-model="sampleText"
        class="input"
        dir="auto"
        placeholder="Type English or Arabic text to test translation and how it appears in-game…"
      />
      <div v-if="transError" class="err">{{ transError }}</div>
      <div class="out-grid">
        <div class="box">
          <div class="lbl">{{ t("aiLogicalLbl") }}</div>
          <pre class="val rtl" dir="rtl">{{ preview.logical }}</pre>
        </div>
        <div class="box">
          <div class="lbl">{{ t("aiVisualLbl") }}</div>
          <pre class="val ltr-override"><template v-for="(seg, i) in parseColorSegments(preview.shaped)" :key="i"><span v-if="seg.color" :style="{ color: seg.color }">{{ seg.text }}</span><template v-else>{{ seg.text }}</template></template></pre>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pane {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 8px 12px 10px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}
.pane-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border-bottom: 1px solid var(--border);
  padding-bottom: 6px;
}
.tabs {
  display: flex;
  align-items: center;
  gap: 4px;
}
.tab {
  height: 24px;
  padding: 0 10px;
  background: transparent;
  color: var(--fg-muted);
  border: 1px solid transparent;
  border-radius: 4px;
  font: inherit;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  cursor: pointer;
}
.tab:hover {
  color: var(--fg);
}
.tab.active {
  background: var(--paper);
  color: var(--fg);
  border-color: var(--border);
}
.btn {
  height: 24px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 4px;
  font: inherit;
  font-size: 11px;
  cursor: pointer;
}
.feed {
  flex: 1;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.feed-item {
  padding: 6px 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-size: 12px;
}
.feed-meta {
  display: flex;
  gap: 8px;
  font-size: 10px;
  color: var(--fg-muted);
  margin-bottom: 2px;
}
.src {
  color: var(--fg-muted);
}
.ar {
  font-family: var(--arabic);
  font-size: 13px;
  margin-top: 2px;
}
.tag {
  color: #4ade80;
}
.err,
.err-tag {
  color: #f87171;
  font-size: 11px;
}
.empty {
  color: var(--fg-muted);
  font-size: 12px;
  padding: 16px 0;
  text-align: center;
}
.tester {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
}
.input {
  height: 30px;
  padding: 0 10px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-family: var(--arabic);
  font-size: 13px;
}
.out-grid {
  flex: 1;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  min-height: 0;
}
.box {
  padding: 6px 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  overflow: auto;
}
.lbl {
  font-size: 10px;
  color: var(--fg-muted);
  margin-bottom: 4px;
}
.val {
  margin: 0;
  font-family: var(--arabic);
  font-size: 13px;
  white-space: pre-wrap;
}
.val.rtl {
  direction: rtl;
  text-align: right;
}
.val.ltr-override {
  direction: ltr;
  unicode-bidi: bidi-override;
  text-align: left;
}
.tag-hint {
  color: var(--fg-muted);
  opacity: 0.7;
  font-size: 11px;
}
.codes {
  font-family: var(--mono);
  font-size: 10px;
  color: var(--fg-faint);
  word-break: break-all;
  line-height: 1.4;
}
.mono {
  font-family: var(--mono);
}
</style>
