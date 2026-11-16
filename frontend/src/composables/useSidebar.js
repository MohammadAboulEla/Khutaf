import { ref, watchEffect } from "vue";

const SIDE_KEY = "app:sidebar:side";
const COLLAPSED_KEY = "app:sidebar:collapsed";

const savedSide = localStorage.getItem(SIDE_KEY);
const side = ref(savedSide === "left" || savedSide === "right" ? savedSide : "left");

const savedCollapsed = localStorage.getItem(COLLAPSED_KEY);
const collapsed = ref(savedCollapsed === null ? true : savedCollapsed === "1");

watchEffect(() => localStorage.setItem(SIDE_KEY, side.value));
watchEffect(() => localStorage.setItem(COLLAPSED_KEY, collapsed.value ? "1" : "0"));

export function useSidebar() {
  const toggle = () => (side.value = side.value === "right" ? "left" : "right");
  const toggleCollapsed = () => (collapsed.value = !collapsed.value);
  return { side, collapsed, toggle, toggleCollapsed };
}
