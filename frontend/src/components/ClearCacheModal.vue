<script setup>
import { computed, onMounted, onUnmounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  games: { type: Array, default: () => [] },
  initialGamePath: { type: String, default: "" },
  totalCount: { type: Number, default: 0 },
});

const emit = defineEmits(["close", "confirm"]);

const { t, isRtl } = useI18n();

const scopeMode = shallowRef("all"); // "game" | "all"
const selectedGamePath = shallowRef("");
const includeManual = shallowRef(false);
const busy = shallowRef(false);

watch(
  () => props.open,
  (isOpen) => {
    if (!isOpen) return;
    busy.value = false;
    includeManual.value = false;
    const hasGames = props.games.length > 0;
    if (props.initialGamePath && hasGames) {
      scopeMode.value = "game";
      selectedGamePath.value = props.initialGamePath;
    } else {
      scopeMode.value = "all";
      selectedGamePath.value = hasGames ? props.games[0].exePath : "";
    }
  },
);

const onKey = (e) => {
  if (e.key === "Escape" && props.open && !busy.value) emit("close");
};
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

const selectedGameTitle = computed(() => {
  const match = props.games.find((g) => g.exePath === selectedGamePath.value);
  if (match?.title) return match.title;
  if (selectedGamePath.value) {
    const parts = selectedGamePath.value.split(/[\\/]/);
    return parts[parts.length - 1] || selectedGamePath.value;
  }
  return "";
});

const descriptionText = computed(() => {
  if (scopeMode.value === "game" && selectedGameTitle.value) {
    return t("ccDescGame", { game: selectedGameTitle.value });
  }
  return t("ccDescAll");
});

const canConfirm = computed(() => {
  if (busy.value) return false;
  if (scopeMode.value === "game") return Boolean(selectedGamePath.value);
  return true;
});

function onConfirm() {
  if (!canConfirm.value) return;
  busy.value = true;
  emit("confirm", {
    gamePath: scopeMode.value === "game" ? selectedGamePath.value : "",
    includeManual: includeManual.value,
  });
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="!busy && emit('close')">
        <div
          class="modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('ccTitle')"
          :dir="isRtl ? 'rtl' : 'ltr'"
        >
          <header class="head">
            <div class="title-wrap">
              <span class="title-ico">
                <Icon name="Trash" :size="15" />
              </span>
              <span class="title">{{ t("ccTitle") }}</span>
            </div>
            <button
              class="close"
              type="button"
              :disabled="busy"
              :aria-label="t('ccCancelBtn')"
              @click="emit('close')"
            >
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <div v-if="games.length" class="scope-box">
              <div class="lbl">{{ t("ccScopeLbl") }}</div>
              <div class="seg">
                <button
                  type="button"
                  class="seg-btn"
                  :class="{ active: scopeMode === 'game' }"
                  @click="scopeMode = 'game'"
                >
                  {{ t("ccScopeGame") }}
                </button>
                <button
                  type="button"
                  class="seg-btn"
                  :class="{ active: scopeMode === 'all' }"
                  @click="scopeMode = 'all'"
                >
                  {{ t("ccScopeAll") }} ({{ totalCount }})
                </button>
              </div>
              <select
                v-if="scopeMode === 'game'"
                v-model="selectedGamePath"
                class="input"
              >
                <option v-for="g in games" :key="g.exePath" :value="g.exePath">
                  {{ g.title }}
                </option>
              </select>
            </div>

            <div class="notice warn">
              <Icon name="AlertTri" :size="15" />
              <span>{{ descriptionText }}</span>
            </div>

            <div class="notice info">
              <Icon name="Info" :size="15" />
              <span>{{ t("ccSafeNote") }}</span>
            </div>

            <label class="chk">
              <input v-model="includeManual" type="checkbox" />
              <span>{{ t("ccIncludeManual") }}</span>
            </label>
          </div>

          <footer class="foot">
            <button class="btn" type="button" :disabled="busy" @click="emit('close')">
              {{ t("ccCancelBtn") }}
            </button>
            <button class="btn danger" type="button" :disabled="!canConfirm" @click="onConfirm">
              <Icon name="Trash" :size="13" />
              <span>{{ t("ccConfirmBtn") }}</span>
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.52);
  display: grid;
  place-items: center;
  z-index: 1050;
  padding: 24px;
}
.modal {
  display: grid;
  grid-template-rows: auto 1fr auto;
  width: min(490px, 100%);
  max-height: min(560px, 90vh);
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border-input, var(--border));
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.42);
  overflow: hidden;
}
.modal[dir="rtl"] {
  font-family: var(--arabic);
}
.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid var(--border);
}
.title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}
.title-ico {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: color-mix(in oklab, #ef4444 16%, transparent);
  color: #f87171;
}
.title {
  font-weight: 600;
  font-size: 14px;
}
.close {
  width: 26px;
  height: 26px;
  display: grid;
  place-items: center;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg);
  cursor: pointer;
}
.close:hover:not(:disabled) {
  background: color-mix(in oklab, var(--fg) 10%, transparent);
}
.body {
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.scope-box {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.lbl {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-muted);
}
.seg {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px;
}
.seg-btn {
  height: 32px;
  padding: 0 10px;
  border-radius: 6px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--paper);
  color: var(--fg-muted);
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.seg-btn.active {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--accent) 14%, var(--paper));
  color: var(--fg);
}
.input {
  height: 32px;
  padding: 0 10px;
  border-radius: 6px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: 12.5px;
}
.notice {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 10px 12px;
  border-radius: 8px;
  font-size: var(--fs-small);
  line-height: 1.55;
  border: 1px solid var(--border);
}
.notice.warn {
  background: color-mix(in oklab, #f59e0b 12%, var(--bg-pill));
  border-color: color-mix(in oklab, #f59e0b 40%, var(--border));
}
.notice.warn > svg {
  color: #f59e0b;
  flex-shrink: 0;
  margin-top: 2px;
}
.notice.info {
  background: var(--bg-pill);
  color: var(--fg-muted);
}
.notice.info > svg {
  color: var(--accent);
  flex-shrink: 0;
  margin-top: 2px;
}
.chk {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: var(--fs-small);
  color: var(--fg-muted);
  cursor: pointer;
}
.chk input {
  accent-color: var(--accent);
}
.foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 13px;
  border-radius: 6px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: var(--fs-small);
  font-weight: 500;
  cursor: pointer;
}
.btn:hover:not(:disabled) {
  background: var(--bg-hover);
}
.btn.danger {
  background: #dc2626;
  border-color: #dc2626;
  color: #fff;
}
.btn.danger:hover:not(:disabled) {
  background: #b91c1c;
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.12s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
