<script setup>
import { computed } from "vue";
import Icon from "./Icon.vue";
import Tooltip from "./Tooltip.vue";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  side: { type: String, default: "right" },
  collapsed: { type: Boolean, default: false },
  active: { type: String, default: "quickplay" },
  simple: { type: Boolean, default: true },
});

const emit = defineEmits(["select", "toggle-collapsed"]);
const { t } = useI18n();

const navItems = computed(() => {
  if (props.simple) {
    return [
      { id: "quickplay", name: t("navQuickplay"), icon: "Gamepad" },
      { id: "translators", name: t("navTranslators"), icon: "Quote" },
      { id: "embed", name: t("navEmbed"), icon: "BookOpen" },
      { id: "hotkeys", name: t("navHotkeys"), icon: "Keyboard" },
    ];
  }
  return [
    { id: "quickplay", name: t("navQuickplay"), icon: "Gamepad" },
    { id: "games", name: t("navGames"), icon: "Folder" },
    { id: "hooks", name: t("navHooks"), icon: "Code" },
    { id: "embed", name: t("navEmbed"), icon: "BookOpen" },
    { id: "translators", name: t("navTranslators"), icon: "Quote" },
    { id: "hotkeys", name: t("navHotkeys"), icon: "Keyboard" },
    { id: "pipeline", name: t("navPipeline"), icon: "Workflow" },
    { id: "debug", name: t("navDebug"), icon: "Activity" },
  ];
});
</script>

<template>
  <aside class="sidebar" :data-side="props.side" :data-collapsed="props.collapsed">
    <nav class="nav">
      <Tooltip
        v-for="i in navItems"
        :key="i.id"
        :text="props.collapsed ? i.name : ''"
        :placement="props.side === 'left' ? 'right' : 'left'"
        class="nav-tip"
      >
        <button
          class="item"
          :class="{ active: props.active === i.id }"
          type="button"
          @click="emit('select', i.id)"
        >
          <Icon :name="i.icon" :size="18" />
          <span class="label">{{ i.name }}</span>
        </button>
      </Tooltip>
    </nav>

    <div class="bottom">
      <Tooltip
        :text="props.collapsed ? t('navExpand') : ''"
        :placement="props.side === 'left' ? 'right' : 'left'"
        class="nav-tip"
      >
        <button
          class="collapser"
          type="button"
          @click="emit('toggle-collapsed')"
        >
          <span class="chev" :class="{ flip: props.collapsed }">
            <Icon name="PanelLeft" :size="18" />
          </span>
          <span class="label">{{ t("navCollapse") }}</span>
        </button>
      </Tooltip>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  display: grid;
  grid-template-rows: 1fr auto;
  background: var(--bg-elev);
  border-right: 1px solid var(--border);
  width: 185px;
  overflow: hidden;
  transition: width 0.15s ease;
}
.sidebar[data-collapsed="true"] {
  width: 50px;
}
.sidebar[data-side="right"] {
  border-right: none;
  border-left: 1px solid var(--border);
}
.nav {
  display: flex;
  flex-direction: column;
  padding: 8px;
  gap: 4px;
  overflow-y: auto;
}
.nav-tip {
  width: 100%;
}
.item,
.collapser {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 8px;
  background: transparent;
  border: none;
  border-radius: 6px;
  color: var(--fg);
  font: inherit;
  text-align: start;
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
  display: none;
}
.chev {
  display: inline-flex;
  transition: transform 0.15s;
}
.chev.flip {
  transform: scaleX(-1);
}
</style>
