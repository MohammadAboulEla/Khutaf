import { ref, watchEffect } from "vue";

const STORAGE_KEY = "app:theme";

const initial = (() => {
  const saved = localStorage.getItem(STORAGE_KEY);
  if (saved === "light" || saved === "dark") return saved;
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
})();

const theme = ref(initial);

watchEffect(() => {
  document.documentElement.dataset.theme = theme.value;
  localStorage.setItem(STORAGE_KEY, theme.value);
});

export function useTheme() {
  const toggle = () => (theme.value = theme.value === "dark" ? "light" : "dark");
  return { theme, toggle };
}
