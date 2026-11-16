<script setup></script>

<template>
  <footer class="shell-footer">
    <slot name="status" />
    <span class="spacer" />
    <slot />
  </footer>
</template>

<style scoped>
.shell-footer {
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
