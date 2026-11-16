<script setup></script>

<template>
  <main class="main">
    <slot>
      <div class="placeholder">Main content</div>
    </slot>
  </main>
</template>

<style scoped>
.main {
  overflow: auto;
  padding: 16px;
  background: var(--bg);
}
.placeholder {
  color: var(--fg-muted);
  font-family: var(--mono);
  font-size: var(--fs-small);
}
</style>
