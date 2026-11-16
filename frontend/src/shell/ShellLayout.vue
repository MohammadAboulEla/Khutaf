<script setup>
defineProps({
  sidebar: { type: String, default: "right" },
});
</script>

<template>
  <div class="shell" :data-sidebar="sidebar">
    <slot name="header" />
    <slot name="aside" />
    <slot />
    <slot name="footer" />
  </div>
</template>

<style scoped>
.shell {
  height: 100%;
  display: grid;
  grid-template-rows: auto 1fr auto;
  grid-template-columns: auto 1fr;
  grid-template-areas:
    "header header"
    "aside  content"
    "footer footer";
}
.shell[data-sidebar="right"] {
  grid-template-columns: 1fr auto;
  grid-template-areas:
    "header  header"
    "content aside"
    "footer  footer";
}
.shell > :slotted([data-area="header"]) { grid-area: header; }
.shell > :slotted([data-area="aside"])  { grid-area: aside; }
.shell > :slotted([data-area="footer"]) { grid-area: footer; }
.shell > :slotted(:not([data-area]))    { grid-area: content; }
</style>
