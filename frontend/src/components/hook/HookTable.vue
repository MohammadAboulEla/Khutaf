<script setup>
import HookBrowser from "./HookBrowser.vue";

defineProps({
  active: { type: Boolean, default: false },
});
const emit = defineEmits(["inspect-text"]);
</script>

<template>
  <HookBrowser variant="advanced" :active="active" @inspect-text="emit('inspect-text', $event)" />
</template>
