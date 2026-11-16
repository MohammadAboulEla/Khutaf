<script setup>
import Icon from "./Icon.vue";

defineProps({
  title: { type: String, default: "Khutفـا" },
  subtitle: { type: String, default: " · Embedded Arabic Hook" },
  version: { type: String, default: "0.1" },
  theme: { type: String, default: "dark" },
  sidebar: { type: String, default: "left" },
  hostReady: { type: Boolean, default: false },
});

const emit = defineEmits(["toggle-theme", "toggle-sidebar", "open-about"]);

const wails = () => window.runtime || (window.go && window.runtime);
const minimize = () => wails()?.WindowMinimise();
const toggleMax = () => wails()?.WindowToggleMaximise();
const close = () => wails()?.Quit();
</script>

<template>
  <header class="app-header drag">
    <div class="brand">
      <div class="brand-mark">Kh</div>
      <div class="ar">{{ title }}</div>
      <div class="en">{{ subtitle }} · v{{ version }}</div>
    </div>

    <div class="crumbs">
      <span class="dot" :class="{ ok: hostReady }" />
      <span>{{ hostReady ? "KhutafHost64 Ready" : "Host Offline" }}</span>
    </div>

    <div class="header-actions no-drag">
      <button class="btn" @click="emit('toggle-theme')">
        <Icon :name="theme === 'dark' ? 'Sun' : 'Moon'" :size="14" />
      </button>
      <button class="btn" @click="emit('toggle-sidebar')">
        <span class="panel-icon" :class="{ flipped: sidebar === 'left' }">
          <Icon name="PanelLeft" :size="14" />
        </span>
      </button>
      <span class="vdivider" />
      <button class="btn" type="button" @click="emit('open-about')">
        <Icon name="Info" :size="12" />
      </button>
      <span class="vdivider" />
      <button class="btn wctl" @click="minimize">
        <Icon name="Minus" :size="14" />
      </button>
      <button class="btn wctl" @click="toggleMax">
        <Icon name="Square" :size="12" />
      </button>
      <button class="btn wctl close" @click="close">
        <Icon name="X" :size="14" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.app-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  height: 40px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-elev);
  padding: 0 12px;
  gap: 16px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  font-weight: 600;
}
.brand-mark {
  width: 24px;
  height: 24px;
  border-radius: 5px;
  background: var(--accent);
  color: #fff;
  display: grid;
  place-items: center;
  font-family: var(--arabic);
  font-size: 13px;
}
.ar {
  font-family: var(--arabic);
  font-size: 15px;
}
.en {
  color: var(--fg-faint);
  font-size: 11px;
  font-family: var(--mono);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.crumbs {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--fg-muted);
  font-size: var(--fs-small);
  font-family: var(--mono);
}
.dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #ef4444;
}
.dot.ok {
  background: #22c55e;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
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
.btn:hover {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
.btn.wctl {
  padding: 0;
  width: 28px;
  justify-content: center;
}
.btn.wctl.close:hover {
  background: #e81123;
  color: #fff;
}
.vdivider {
  width: 1px;
  height: 16px;
  background: var(--border);
  margin: 0 4px;
}
.panel-icon {
  display: inline-flex;
  transition: transform 0.15s;
}
.panel-icon.flipped {
  transform: scaleX(-1);
}
</style>
