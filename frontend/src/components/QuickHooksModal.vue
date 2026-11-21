<script setup>
import { computed, onBeforeUnmount, onMounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  standalone: { type: Boolean, default: false },
});

const emit = defineEmits(["close"]);
const { t } = useI18n();
const { hooks, hookOutputs, status, config, setEmbed, refreshHooks } = useSession();

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) refreshHooks();
  },
  { immediate: true },
);

const search = shallowRef("");

function onKeydown(e) {
  if (props.open && e.key === "Escape") {
    e.preventDefault();
    emit("close");
  }
}

onMounted(() => window.addEventListener("keydown", onKeydown));
onBeforeUnmount(() => window.removeEventListener("keydown", onKeydown));

const embeddableHooks = computed(() => {
  const q = search.value.trim().toLowerCase();
  const list = hooks.value.filter((h) => h.embeddable);
  const filtered = q
    ? list.filter((h) => {
        const text = (hookOutputs.value[h.key] || "").toLowerCase();
        const name = (h.hookName || "").toLowerCase();
        const code = (h.hookCode || "").toLowerCase();
        return text.includes(q) || name.includes(q) || code.includes(q);
      })
    : list;

  // Keep enabled (checked) hooks at the top of the list.
  return [...filtered].sort((a, b) => {
    if (Boolean(a.embedding) !== Boolean(b.embedding)) {
      return a.embedding ? -1 : 1;
    }
    return 0;
  });
});

const activeCount = computed(() => hooks.value.filter((h) => h.embeddable && h.embedding).length);
</script>

<template>
  <Teleport :disabled="standalone" to="body">
    <Transition name="fade">
      <div
        v-if="open"
        :class="standalone ? 'standalone-host' : 'modal-overlay'"
        @click.self="!standalone && emit('close')"
      >
        <div
          class="modal-box"
          :class="{ standalone }"
          role="dialog"
          aria-modal="true"
          :aria-label="t('qhTitle')"
        >
          <header class="modal-head" :class="{ drag: standalone }">
            <div class="title-col">
              <div class="modal-title">{{ t("qhTitle") }}</div>
              <div class="modal-sub">
                {{ status.attached ? t("qhSubAttached", { game: status.gameName, active: activeCount }) : t("qhSubIdle") }}
              </div>
            </div>
            <button
              class="modal-close"
              :class="{ 'no-drag': standalone }"
              type="button"
              :aria-label="t('qhClose')"
              @click="emit('close')"
            >
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="toolbar">
            <input
              v-model="search"
              class="search-input"
              type="text"
              :placeholder="t('qhSearchPlaceholder')"
            />
          </div>

          <div class="modal-body">
            <div v-if="!status.attached" class="empty-msg">{{ t("qhNotAttached") }}</div>
            <div v-else-if="!embeddableHooks.length" class="empty-msg">{{ t("qhEmpty") }}</div>
            <div v-else class="hooks-list">
              <label
                v-for="h in embeddableHooks"
                :key="h.key"
                class="hook-row"
                :class="{ active: h.embedding }"
              >
                <input
                  type="checkbox"
                  :checked="h.embedding"
                  @change="setEmbed(h.key, $event.target.checked)"
                />
                <div class="hook-info">
                  <div class="hook-primary">
                    <div
                      class="hook-sample"
                      :class="{ empty: !hookOutputs[h.key] }"
                      dir="auto"
                    >
                      {{ hookOutputs[h.key] || "—" }}
                    </div>
                    <span v-if="!h.embedding" class="badge new">{{ t("qhNewBadge") }}</span>
                    <span v-else class="badge on">{{ t("qhActiveBadge") }}</span>
                  </div>
                </div>
              </label>
            </div>
          </div>

          <footer class="modal-foot">
            <span class="hint-text">{{ t("qhHotkeyHint", { key: config?.hotkeys?.toggleHooksMenu || "Ctrl+Alt+H" }) }}</span>
            <button class="btn secondary" type="button" @click="emit('close')">{{ t("qhClose") }}</button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.standalone-host {
  width: 100%;
  height: 100%;
  display: flex;
}
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 9200;
  background: rgba(0, 0, 0, 0.62);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
}
.modal-box {
  width: 100%;
  max-width: 620px;
  max-height: 82vh;
  display: flex;
  flex-direction: column;
  background: var(--bg-elev);
  border: 1px solid var(--border);
  border-radius: 10px;
  box-shadow: 0 20px 50px rgba(0, 0, 0, 0.45);
  overflow: hidden;
}
.modal-box.standalone {
  max-width: none;
  max-height: none;
  height: 100%;
  border-radius: 0;
  box-shadow: none;
}
.drag {
  --wails-draggable: drag;
  cursor: move;
}
.no-drag {
  --wails-draggable: no-drag;
  cursor: pointer;
}
.modal-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.modal-title {
  font-size: 14px;
  font-weight: 700;
}
.modal-sub {
  font-size: 11.5px;
  color: var(--fg-muted);
  margin-top: 2px;
}
.modal-close {
  background: transparent;
  border: none;
  color: var(--fg-muted);
  cursor: pointer;
  padding: 4px;
}
.toolbar {
  display: flex;
  gap: 8px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--border);
  background: var(--bg);
}
.search-input {
  flex: 1;
  min-width: 0;
  height: 30px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.modal-body {
  flex: 1;
  min-height: 160px;
  overflow: auto;
  padding: 12px 16px;
}
.empty-msg {
  text-align: center;
  padding: 28px 12px;
  color: var(--fg-muted);
  font-size: 12.5px;
}
.hooks-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.hook-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 9px 11px;
  border: 1px solid var(--border);
  border-radius: 7px;
  background: var(--bg);
  cursor: pointer;
}
.hook-row.active {
  border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  background: color-mix(in srgb, var(--accent) 7%, var(--bg));
}
.hook-row input[type="checkbox"] {
  margin-top: 3px;
  accent-color: var(--accent);
}
.hook-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.hook-primary {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
}
.hook-sample {
  flex: 1;
  min-width: 0;
  font-size: 13.5px;
  font-weight: 600;
  line-height: 1.45;
  color: var(--fg);
  white-space: pre-wrap;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
  max-height: 64px;
}
.hook-sample.empty {
  color: var(--fg-muted);
  font-weight: 400;
  font-style: italic;
}
.badge {
  flex: none;
  font-size: 10.5px;
  padding: 2px 7px;
  border-radius: 4px;
  font-weight: 600;
}
.badge.new {
  background: rgba(245, 158, 11, 0.16);
  color: #f59e0b;
}
.badge.on {
  background: color-mix(in srgb, var(--accent) 18%, transparent);
  color: var(--accent);
}
.modal-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 16px;
  border-top: 1px solid var(--border);
}
.hint-text {
  font-size: 11px;
  color: var(--fg-muted);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--fg);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
