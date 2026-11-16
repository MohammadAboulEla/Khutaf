<script setup></script>

<template>
  <main class="shell-main">
    <slot />
  </main>
</template>

<style scoped>
.shell-main {
  overflow: auto;
  padding: 16px;
  background: var(--bg);
}
</style>
