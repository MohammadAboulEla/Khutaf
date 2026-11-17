<script setup>
defineProps({
  status: { type: String, default: "Ready" },
  version: { type: String, default: "0.1.0" },
});
</script>

<template>
  <footer class="footer">
    <span class="status">{{ status }}</span>
    <span class="spacer" />
    <span class="meta">v{{ version }}</span>
  </footer>
</template>

<style scoped>
.footer {
  display: flex;
  align-items: center;
  gap: 8px;
  border-top: 1px solid var(--border);
  background: var(--bg-elev);
  padding: 4px 12px;
  font-family: var(--mono);
  font-size: var(--fs-small);
  color: var(--fg-muted);
}
.spacer { flex: 1; }
</style>
