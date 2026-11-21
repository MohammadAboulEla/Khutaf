import { computed, shallowRef, watch } from "vue";

export function usePinnedHookSort(getHooks) {
  const pinnedKeys = shallowRef(new Set());
  let initialized = false;

  function snapshotPinned() {
    const list = getHooks() || [];
    pinnedKeys.value = new Set(list.filter((h) => h.embedding).map((h) => h.key));
    initialized = true;
  }

  // Re-initialize automatically when hooks first arrive for a newly attached game.
  watch(
    () => (getHooks() || []).length,
    (len) => {
      if (len === 0) {
        initialized = false;
        if (pinnedKeys.value.size > 0) {
          pinnedKeys.value = new Set();
        }
      } else if (!initialized) {
        snapshotPinned();
      }
    },
    { immediate: true },
  );

  const needsSort = computed(() => {
    const list = getHooks() || [];
    const pinned = pinnedKeys.value;
    for (const h of list) {
      if (Boolean(h.embedding) !== pinned.has(h.key)) {
        return true;
      }
    }
    return false;
  });

  function sortPinned(list) {
    const pinned = pinnedKeys.value;
    return [...list].sort((a, b) => {
      const pa = pinned.has(a.key);
      const pb = pinned.has(b.key);
      if (pa === pb) return 0;
      return pa ? -1 : 1;
    });
  }

  return { snapshotPinned, sortPinned, needsSort };
}
