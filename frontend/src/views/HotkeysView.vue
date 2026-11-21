<script setup>
import HotkeysPanel from "../components/HotkeysPanel.vue";
</script>

<template>
  <div class="page">
    <div class="card">
      <HotkeysPanel />
    </div>
  </div>
</template>

<style scoped>
.page {
  padding: 16px;
  overflow: auto;
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.card {
  width: 100%;
  padding: 16px 18px;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 6px;
}
</style>
