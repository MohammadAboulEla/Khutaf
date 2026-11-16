<script setup>
import { shallowRef, watch } from "vue";

const props = defineProps({
  feed: { type: Array, required: true },
  previewFn: { type: Function, required: true },
  translateFn: { type: Function, required: true },
});

const sampleText = shallowRef("مرحبا بك يا Ahmed في عالم <color=red>Khutaf</color>! [HP: 100]");
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
    if (res) {
      sampleText.value = res.translated;
    }
  } catch (e) {
    transError.value = String(e);
  } finally {
    transBusy.value = false;
  }
}
</script>

<template>
  <section class="inspector">
    <div class="pane">
      <div class="pane-head">
        <span class="title">Arabic Shaping &amp; BiDi Tester</span>
        <button class="btn" type="button" :disabled="transBusy" @click="onTestTranslate">
          {{ transBusy ? "Translating…" : "Translate with Active Provider" }}
        </button>
      </div>
      <textarea
        v-model="sampleText"
        class="input"
        rows="2"
        placeholder="Type Arabic or source dialogue to inspect shaping & BiDi ordering…"
      />
      <div v-if="transError" class="err">{{ transError }}</div>
      <div class="out-grid">
        <div class="box">
          <div class="lbl">Logical Wrapped (RTL)</div>
          <pre class="val rtl">{{ preview.logical }}</pre>
        </div>
        <div class="box">
          <div class="lbl">In-Game Visual Buffer (Shaped LTR + Codepoints)</div>
          <pre class="val">{{ preview.shaped }}</pre>
          <div class="codes">{{ preview.codes }}</div>
        </div>
      </div>
    </div>

    <div class="pane">
      <div class="pane-head">
        <span class="title">Live Embedded Feed ({{ props.feed.length }})</span>
      </div>
      <div class="feed">
        <div v-if="!props.feed.length" class="empty">
          Lines injected into the game appear here in real time.
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
    </div>
  </section>
</template>

<style scoped>
.inspector {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  min-height: 195px;
}
.pane {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px 12px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
  overflow: hidden;
}
.pane-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--fg-muted);
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
.input {
  width: 100%;
  padding: 6px 8px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font-family: var(--arabic);
  font-size: 13px;
  resize: none;
}
.out-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
  flex: 1;
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
.codes {
  margin-top: 4px;
  font-family: var(--mono);
  font-size: 10px;
  color: var(--fg-faint);
  word-break: break-all;
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
.mono {
  font-family: var(--mono);
}
</style>
