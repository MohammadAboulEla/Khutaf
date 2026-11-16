<script setup>
import { nextTick, onBeforeUnmount, ref, shallowRef } from "vue";

const props = defineProps({
  text: { type: String, default: "" },
  placement: { type: String, default: "top" }, // top | bottom | left | right
  delay: { type: Number, default: 180 },
  mono: { type: Boolean, default: false },
});

const triggerRef = ref(null);
const tipRef = ref(null);
const visible = shallowRef(false);
const pos = shallowRef({ top: "0px", left: "0px" });
let timer = null;

function clearTimer() {
  if (timer !== null) {
    clearTimeout(timer);
    timer = null;
  }
}

async function updatePosition() {
  await nextTick();
  const el = triggerRef.value;
  const tip = tipRef.value;
  if (!el || !tip) return;

  const r = el.getBoundingClientRect();
  const tr = tip.getBoundingClientRect();
  const gap = 6;
  const pad = 8;

  let top = 0;
  let left = 0;
  if (props.placement === "bottom") {
    top = r.bottom + gap;
    left = r.left + (r.width - tr.width) / 2;
  } else if (props.placement === "left") {
    top = r.top + (r.height - tr.height) / 2;
    left = r.left - tr.width - gap;
  } else if (props.placement === "right") {
    top = r.top + (r.height - tr.height) / 2;
    left = r.right + gap;
  } else {
    top = r.top - tr.height - gap;
    left = r.left + (r.width - tr.width) / 2;
    if (top < pad) top = r.bottom + gap;
  }

  left = Math.max(pad, Math.min(left, window.innerWidth - tr.width - pad));
  top = Math.max(pad, Math.min(top, window.innerHeight - tr.height - pad));
  pos.value = { top: `${Math.round(top)}px`, left: `${Math.round(left)}px` };
}

function show() {
  if (!props.text) return;
  clearTimer();
  timer = setTimeout(() => {
    visible.value = true;
    updatePosition();
  }, props.delay);
}

function hide() {
  clearTimer();
  visible.value = false;
}

onBeforeUnmount(clearTimer);
</script>

<template>
  <span
    ref="triggerRef"
    class="tt-trigger"
    @mouseenter="show"
    @mouseleave="hide"
    @focusin="show"
    @focusout="hide"
    @mousedown="hide"
  >
    <slot />
    <Teleport to="body">
      <Transition name="tt">
        <div
          v-if="visible && text"
          ref="tipRef"
          class="tt-bubble"
          :class="{ mono }"
          :data-placement="placement"
          :style="pos"
          role="tooltip"
        >
          {{ text }}
        </div>
      </Transition>
    </Teleport>
  </span>
</template>

<style scoped>
.tt-trigger {
  display: inline-flex;
  min-width: 0;
  max-width: 100%;
}
</style>

<style>
.tt-bubble {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  max-width: min(440px, calc(100vw - 16px));
  padding: 5px 9px;
  border-radius: 6px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border-input, var(--border));
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.22);
  font-family: var(--sans);
  font-size: 11.5px;
  font-weight: 500;
  line-height: 1.35;
  word-break: break-word;
}
.tt-bubble.mono {
  font-family: var(--mono);
  font-size: 11px;
}
.tt-enter-active,
.tt-leave-active {
  transition: opacity 0.12s ease, transform 0.12s ease;
}
.tt-enter-from,
.tt-leave-to {
  opacity: 0;
  transform: translateY(2px);
}
</style>
