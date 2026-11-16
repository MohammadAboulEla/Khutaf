<script setup>
import Icon from "./Icon.vue";

const props = defineProps({
  side: { type: String, default: "right" },
  collapsed: { type: Boolean, default: false },
  active: { type: String, default: "hooks" },
  items: {
    type: Array,
    default: () => [
      { id: "hooks", name: "Hook Console", icon: "Code" },
      { id: "embed", name: "Embed & Arabic", icon: "BookOpen" },
      { id: "translators", name: "Translators", icon: "Quote" },
      { id: "pipeline", name: "Text Pipeline", icon: "Cog" },
    ],
  },
});

const emit = defineEmits(["select", "toggle-collapsed"]);
</script>

<template>
  <aside class="sidebar" :data-side="props.side" :data-collapsed="props.collapsed">
    <nav class="nav">
      <button
        v-for="i in props.items"
        :key="i.id"
        class="item"
        :class="{ active: props.active === i.id }"
        type="button"
        :title="props.collapsed ? i.name : undefined"
        @click="emit('select', i.id)"
      >
        <Icon :name="i.icon" :size="18" />
        <span class="label">{{ i.name }}</span>
      </button>
    </nav>

    <div class="bottom">
      <button
        class="collapser"
        type="button"
        :title="props.collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        @click="emit('toggle-collapsed')"
      >
        <span class="chev" :class="{ flip: props.collapsed }">
          <Icon name="PanelLeft" :size="18" />
        </span>
        <span class="label">Collapse</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  display: grid;
  grid-template-rows: 1fr auto;
  background: var(--bg-elev);
  border-right: 1px solid var(--border);
  width: 210px;
  overflow: hidden;
  transition: width 0.15s ease;
}
.sidebar[data-collapsed="true"] {
  width: 48px;
}
.sidebar[data-side="right"] {
  border-right: none;
  border-left: 1px solid var(--border);
}
.nav {
  display: flex;
  flex-direction: column;
  padding: 8px;
  gap: 2px;
  overflow-y: auto;
}
.item,
.collapser {
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
.item:hover,
.collapser:hover {
  background: color-mix(in oklab, var(--fg) 8%, transparent);
}
.item.active {
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
  font-weight: 600;
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
.sidebar[data-collapsed="true"] .label {
  opacity: 0;
  pointer-events: none;
}
.chev {
  display: inline-flex;
  transition: transform 0.15s;
}
.chev.flip {
  transform: scaleX(-1);
}
</style>
