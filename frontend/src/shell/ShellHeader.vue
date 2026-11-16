<script setup>
import Icon from "../components/Icon.vue";

defineProps({
  theme: { type: String, default: "light" },
  sidebar: { type: String, default: "right" },
});

defineEmits(["toggle-theme", "toggle-sidebar", "open-about"]);

const wails = () => window.runtime || (window.go && window.runtime);
const minimize = () => wails()?.WindowMinimise();
const toggleMax = () => wails()?.WindowToggleMaximise();
const close = () => wails()?.Quit();
</script>

<template>
  <header class="shell-header drag" data-area="header">
    <div class="brand">
      <slot name="brand" />
    </div>

    <div class="spacer" />

    <div class="actions no-drag">
      <slot name="actions" />
      <button class="btn" title="theme" @click="$emit('toggle-theme')">
        <Icon :name="theme === 'dark' ? 'Sun' : 'Moon'" :size="14" />
      </button>
      <button class="btn" title="toggle sidebar side" @click="$emit('toggle-sidebar')">
        <span class="panel-icon" :class="{ flipped: sidebar === 'left' }">
          <Icon name="PanelLeft" :size="14" />
        </span>
      </button>
      <span class="vdivider" />
      <button class="btn" type="button" @click="$emit('open-about')">
        <Icon name="Info" :size="12" />
      </button>
      <span class="vdivider" />
      <button class="btn wctl" title="minimize" @click="minimize">
        <Icon name="Minus" :size="14" />
      </button>
      <button class="btn wctl" title="maximize" @click="toggleMax">
        <Icon name="Square" :size="12" />
      </button>
      <button class="btn wctl close" title="close" @click="close">
        <Icon name="X" :size="14" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.shell-header {
  display: flex;
  align-items: center;
  height: 40px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elev);
  padding: 0 12px;
  gap: 16px;
}

.brand { display: flex; align-items: center; gap: 10px; font-weight: 600; }
.spacer { flex: 1; }

.actions { display: flex; align-items: center; gap: 4px; }

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  min-width: 26px;
  padding: 0 8px;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg);
  font: inherit;
  font-size: var(--fs-small);
  cursor: pointer;
}
.btn:hover { background: color-mix(in oklab, var(--fg) 10%, transparent); }
.btn.wctl { padding: 0; width: 28px; justify-content: center; }
.btn.wctl.close:hover { background: #e81123; color: #fff; }

.vdivider {
  width: 1px; height: 16px;
  background: var(--border);
  margin: 0 4px;
}

.panel-icon { display: inline-flex; transition: transform 0.15s; }
.panel-icon.flipped { transform: scaleX(-1); }
</style>
