import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from "vue";
import { usePinnedHookSort } from "./usePinnedHookSort.js";

const normPath = (p) => (p || "").toLowerCase().replace(/\//g, "\\");

const CATEGORY_ORDER = ["recommended", "active", "ui", "all", "noise", "muted"];

/**
 * Unified state, filtering, classification tabs, temporary-history search, freeze,
 * sorting, and keyboard navigation for HookBrowser across Simple, Picker, and Advanced views.
 */
export function useHookBrowser({
  hooks,
  hookOutputs,
  hookHistory,
  hookPulses,
  savedGames,
  status,
  variant = "picker",
}) {
  const search = shallowRef("");
  const activeCategory = shallowRef("all");
  const userPickedCategory = shallowRef(false);
  const showSavedOnly = shallowRef(false);
  const embedOnly = shallowRef(variant !== "advanced");
  const sortMode = shallowRef("recommended"); // "recommended" | "recent" | "pinned"
  const frozen = shallowRef(false);
  const frozenSnapshot = shallowRef(null);
  const expandedKeys = ref({});
  const selectedIndex = shallowRef(0);
  const nowTick = shallowRef(Date.now());
  const lastPresetUndo = shallowRef(null);

  let tickTimer = null;
  onMounted(() => {
    tickTimer = setInterval(() => {
      if (!frozen.value) nowTick.value = Date.now();
    }, 1000);
  });
  onBeforeUnmount(() => {
    if (tickTimer) clearInterval(tickTimer);
  });

  const liveHooksSource = computed(() =>
    frozen.value && frozenSnapshot.value ? frozenSnapshot.value.hooks : hooks.value || [],
  );
  const outputsSource = computed(() =>
    frozen.value && frozenSnapshot.value ? frozenSnapshot.value.outputs : hookOutputs.value || {},
  );
  const historySource = computed(() =>
    frozen.value && frozenSnapshot.value ? frozenSnapshot.value.history : hookHistory.value || {},
  );

  const { snapshotPinned, sortPinned, needsSort } = usePinnedHookSort(() => liveHooksSource.value);

  function toggleFreeze() {
    if (frozen.value) {
      frozen.value = false;
      frozenSnapshot.value = null;
      nowTick.value = Date.now();
      return;
    }
    const snapHistory = {};
    for (const [k, list] of Object.entries(hookHistory.value || {})) {
      snapHistory[k] = Array.isArray(list) ? [...list] : [];
    }
    frozenSnapshot.value = {
      hooks: (hooks.value || []).map((h) => ({ ...h })),
      outputs: { ...(hookOutputs.value || {}) },
      history: snapHistory,
    };
    frozen.value = true;
  }

  function resetFreeze() {
    frozen.value = false;
    frozenSnapshot.value = null;
  }

  // Keep savedOnlyHooks synchronized for the currently attached game.
  const savedOnlyHooks = computed(() => {
    if (!status.value?.attached || !status.value?.gamePath) return [];
    const curPath = normPath(status.value.gamePath);
    const game = (savedGames.value || []).find((g) => normPath(g.exePath) === curPath);
    if (!game?.embedHooks?.length) return [];

    const liveIds = new Set(
      liveHooksSource.value.map((h) => `${h.hookCode}|${h.ctx || 0}|${h.ctx2 || 0}`),
    );
    return game.embedHooks
      .filter((s) => !liveIds.has(`${s.hookCode}|${s.ctx || 0}|${s.ctx2 || 0}`))
      .map((s) => ({
        key: `saved:${s.hookCode}:${(s.ctx || 0).toString(16)}:${(s.ctx2 || 0).toString(16)}`,
        hookCode: s.hookCode,
        hookName: s.hookName || s.hookCode,
        ctx: s.ctx || 0,
        ctx2: s.ctx2 || 0,
        embeddable: true,
        embedding: true,
        muted: false,
        savedOnly: true,
        category: "dialogue",
        dialogueScore: 70,
        sample: s.sample || "",
        objectPath: s.path || "",
        label: s.label || "",
        hitCount: 0,
        updatesPerSec: 0,
        lastSeenMs: 0,
      }));
  });

  const baseLiveHooks = computed(() =>
    liveHooksSource.value.filter((h) => !embedOnly.value || h.embeddable),
  );

  // Category counters (for live filter chips).
  const counts = computed(() => {
    let recommended = 0;
    let active = 0;
    let ui = 0;
    let noise = 0;
    let muted = 0;
    let nonMuted = 0;

    for (const h of baseLiveHooks.value) {
      if (h.muted) {
        muted++;
        continue;
      }
      if (h.embedding) active++;
      const cat = h.category || "unknown";
      if (cat === "noise") {
        noise++;
        continue;
      }
      nonMuted++;
      if (cat === "dialogue" || cat === "speaker") recommended++;
      else if (cat === "ui") ui++;
    }

    const saved = savedOnlyHooks.value.length;
    return {
      recommended,
      active: showSavedOnly.value ? active + saved : active,
      ui,
      all: showSavedOnly.value ? nonMuted + saved : nonMuted,
      noise,
      muted,
      saved,
    };
  });

  // Auto-select "recommended" when dialogue/speaker hooks first appear unless user already picked a tab.
  watch(
    () => counts.value.recommended,
    (recCount, prev) => {
      if (!userPickedCategory.value && recCount > 0 && (!prev || prev === 0)) {
        activeCategory.value = "recommended";
      } else if (activeCategory.value === "recommended" && recCount === 0) {
        activeCategory.value = "all";
      }
    },
    { immediate: true },
  );

  // Reset category auto-selection when detaching or switching games.
  watch(
    () => status.value?.gamePath,
    () => {
      userPickedCategory.value = false;
      activeCategory.value = "all";
      lastPresetUndo.value = null;
      resetFreeze();
    },
  );

  function setCategory(cat) {
    activeCategory.value = cat;
    userPickedCategory.value = true;
    selectedIndex.value = 0;
  }

  function cycleCategory(step = 1) {
    const available = CATEGORY_ORDER.filter((cat) => {
      if (cat === "noise") return counts.value.noise > 0;
      if (cat === "muted") return counts.value.muted > 0;
      return true;
    });
    const idx = available.indexOf(activeCategory.value);
    const next = available[(idx + step + available.length) % available.length];
    setCategory(next);
  }

  function getHookHistoryList(h) {
    if (h.savedOnly) {
      return h.sample ? [{ text: h.sample, atMs: 0 }] : [];
    }
    const list = historySource.value[h.key];
    if (Array.isArray(list) && list.length) return list;
    const cur = outputsSource.value[h.key] || h.lastText;
    return cur ? [{ text: cur, atMs: h.lastSeenMs || 0 }] : [];
  }

  function isHookPulsing(key) {
    if (frozen.value) return false;
    const ts = hookPulses.value?.[key];
    return Boolean(ts && nowTick.value - ts <= 1500);
  }

  const displayedHooks = computed(() => {
    const q = search.value.trim().toLowerCase();
    const cat = activeCategory.value;

    let pool = baseLiveHooks.value;
    if (showSavedOnly.value && (cat === "all" || cat === "active" || cat === "recommended")) {
      pool = [...pool, ...savedOnlyHooks.value];
    }

    // Category filter (when searching, search across non-muted hooks including noise so user can find anything).
    const catFiltered = pool.filter((h) => {
      if (cat === "muted") return Boolean(h.muted);
      if (h.muted) return false;
      if (q) return true;
      const hc = h.category || "unknown";
      if (cat === "noise") return hc === "noise";
      if (cat === "recommended") return hc === "dialogue" || hc === "speaker" || Boolean(h.savedOnly);
      if (cat === "active") return Boolean(h.embedding);
      if (cat === "ui") return hc === "ui";
      // "all" hides noise unless explicitly switched to noise tab or searched.
      return hc !== "noise";
    });

    // Search filter (current text + full temporary history + GameObject path + name/code/ctx).
    const annotated = [];
    for (const h of catFiltered) {
      const currentText = h.savedOnly ? h.sample || "" : outputsSource.value[h.key] || h.lastText || "";
      const hist = getHookHistoryList(h);
      let matchedHistory = "";

      if (q) {
        const curLow = currentText.toLowerCase();
        const nameLow = (h.hookName || "").toLowerCase();
        const codeLow = (h.hookCode || "").toLowerCase();
        const pathLow = (h.objectPath || h.path || "").toLowerCase();
        const ctxHex = h.ctx ? h.ctx.toString(16).toLowerCase() : "";

        const directMatch =
          curLow.includes(q) ||
          nameLow.includes(q) ||
          codeLow.includes(q) ||
          pathLow.includes(q) ||
          ctxHex.includes(q);

        if (!directMatch) {
          const histHit = hist.find((item) => (item.text || "").toLowerCase().includes(q));
          if (!histHit) continue;
          matchedHistory = histHit.text;
        }
      }

      annotated.push({
        ...h,
        _currentText: currentText,
        _history: hist,
        _matchedHistory: matchedHistory,
      });
    }

    // Sort according to sortMode.
    if (sortMode.value === "pinned") {
      return sortPinned(annotated);
    }
    if (sortMode.value === "recent") {
      return [...annotated].sort((a, b) => {
        const ta = a.lastSeenMs || 0;
        const tb = b.lastSeenMs || 0;
        if (ta !== tb) return tb - ta;
        return (b.dialogueScore || 0) - (a.dialogueScore || 0);
      });
    }
    // "recommended": Active first -> Dialogue/Speaker score -> Recent activity.
    return [...annotated].sort((a, b) => {
      if (Boolean(a.embedding) !== Boolean(b.embedding)) {
        return a.embedding ? -1 : 1;
      }
      const sa = a.dialogueScore || 0;
      const sb = b.dialogueScore || 0;
      if (sa !== sb) return sb - sa;
      return (b.lastSeenMs || 0) - (a.lastSeenMs || 0);
    });
  });

  watch(
    () => displayedHooks.value.length,
    (len) => {
      if (len === 0) selectedIndex.value = 0;
      else if (selectedIndex.value >= len) selectedIndex.value = len - 1;
    },
  );

  function toggleExpand(key) {
    expandedKeys.value = {
      ...expandedKeys.value,
      [key]: !expandedKeys.value[key],
    };
  }

  function cycleSortMode() {
    const order = ["recommended", "recent", "pinned"];
    const next = order[(order.indexOf(sortMode.value) + 1) % order.length];
    sortMode.value = next;
    if (next === "pinned") snapshotPinned();
  }

  function moveSelection(delta) {
    const len = displayedHooks.value.length;
    if (!len) return;
    selectedIndex.value = (selectedIndex.value + delta + len) % len;
  }

  return {
    search,
    activeCategory,
    showSavedOnly,
    embedOnly,
    sortMode,
    frozen,
    expandedKeys,
    selectedIndex,
    nowTick,
    lastPresetUndo,
    savedOnlyHooks,
    counts,
    displayedHooks,
    needsSort,
    snapshotPinned,
    setCategory,
    cycleCategory,
    cycleSortMode,
    toggleFreeze,
    resetFreeze,
    toggleExpand,
    isHookPulsing,
    moveSelection,
  };
}
