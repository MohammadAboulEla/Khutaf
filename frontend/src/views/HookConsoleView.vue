<script setup>
import { shallowRef, onUnmounted } from "vue";
import ProcessBar from "../components/hook/ProcessBar.vue";
import HookTable from "../components/hook/HookTable.vue";
import ArabicInspector from "../components/hook/ArabicInspector.vue";
import { useSession } from "../composables/useSession.js";

defineProps({
  active: { type: Boolean, default: false },
});

const {
  status,
  processes,
  hooks,
  hookOutputs,
  embedFeed,
  refreshProcesses,
  attach,
  detach,
  setEmbed,
  previewArabic,
  testTranslate,
} = useSession();

const STORAGE_KEY = "khutaf:inspectorHeight";
const saved = Number(localStorage.getItem(STORAGE_KEY));
const inspectorHeight = shallowRef(
  Number.isFinite(saved) && saved >= 90 ? saved : 190,
);
const dragging = shallowRef(false);

let startY = 0;
let startH = 0;

function onDragMove(e) {
  const delta = startY - e.clientY;
  const next = Math.max(90, Math.min(460, startH + delta));
  inspectorHeight.value = next;
}

function onDragEnd() {
  dragging.value = false;
  localStorage.setItem(STORAGE_KEY, String(inspectorHeight.value));
  window.removeEventListener("mousemove", onDragMove);
  window.removeEventListener("mouseup", onDragEnd);
}

function startDrag(e) {
  dragging.value = true;
  startY = e.clientY;
  startH = inspectorHeight.value;
  window.addEventListener("mousemove", onDragMove);
  window.addEventListener("mouseup", onDragEnd);
}

onUnmounted(() => {
  window.removeEventListener("mousemove", onDragMove);
  window.removeEventListener("mouseup", onDragEnd);
});
</script>

<template>
  <div class="view" :class="{ resizing: dragging }">
    <ProcessBar
      :status="status"
      :processes="processes"
      @refresh="refreshProcesses"
      @attach="attach"
      @detach="detach"
    />

    <HookTable
      class="hooks-pane"
      :hooks="hooks"
      :outputs="hookOutputs"
      :active="active"
      @toggle-embed="setEmbed"
    />

    <div
      class="splitter"
      @mousedown.prevent="startDrag"
    >
      <span class="grip" />
    </div>

    <ArabicInspector
      class="inspector-pane"
      :style="{ height: inspectorHeight + 'px' }"
      :feed="embedFeed"
      :preview-fn="previewArabic"
      :translate-fn="testTranslate"
    />
  </div>
</template>

<style scoped>
.view {
  display: flex;
  flex-direction: column;
  gap: 8px;
  height: 100%;
  padding: 12px;
  overflow: hidden;
}
.view.resizing {
  user-select: none;
  cursor: row-resize;
}
.hooks-pane {
  flex: 1 1 0;
  min-height: 140px;
}
.splitter {
  flex: 0 0 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: row-resize;
  margin: -4px 0;
}
.grip {
  width: 44px;
  height: 3px;
  border-radius: 2px;
  background: var(--border);
  transition: background 0.15s;
}
.splitter:hover .grip,
.view.resizing .grip {
  background: var(--accent);
}
.inspector-pane {
  flex: 0 0 auto;
  min-height: 0;
}
</style>
