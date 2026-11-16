<script setup>
import Icon from "../components/Icon.vue";

defineProps({
  collapsed: { type: Boolean, default: false },
});

defineEmits(["toggle-collapsed"]);
</script>

<template>
  <aside class="shell-sidebar" data-area="aside" :data-collapsed="collapsed">
    <nav>
      <slot />
    </nav>

    <div class="bottom">
      <button
        class="item"
        type="button"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="$emit('toggle-collapsed')"
      >
        <span class="chev" :class="{ flip: collapsed }">
          <Icon name="PanelLeft" :size="18" />
        </span>
        <span class="label">Collapse</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.shell-sidebar {
  display: grid;
  grid-template-rows: 1fr auto;
  background: var(--bg-elev);
  border-right: 1px solid var(--border);
  width: 220px;
  overflow: hidden;
  transition: width 0.15s ease;
}
.shell-sidebar[data-collapsed="true"] { width: 48px; }

nav {
  display: flex;
  flex-direction: column;
  padding: 8px;
  gap: 2px;
  overflow-y: auto;
}

.item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  background: transparent;
  border: none;
  border-radius: 6px;
  color: var(--fg);
  font: inherit;
  text-align: left;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
}
.item:hover {
  background: color-mix(in oklab, var(--fg) 8%, transparent);
}

.bottom {
  display: flex;
  flex-direction: column;
  padding: 8px;
  gap: 2px;
  border-top: 1px solid var(--border);
}

.item > :first-child {
  flex: 0 0 18px;
  display: inline-flex;
  justify-content: center;
}
.label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: opacity 0.12s;
}
.shell-sidebar[data-collapsed="true"] .label {
  opacity: 0;
  pointer-events: none;
}

.chev { display: inline-flex; transition: transform 0.15s; }
.chev.flip { transform: scaleX(-1); }
</style>
