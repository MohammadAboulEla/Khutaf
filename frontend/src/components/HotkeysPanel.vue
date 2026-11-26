<script setup>
import { reactive, shallowRef, watch } from "vue";
import Tooltip from "./Tooltip.vue";
import { useSession } from "../composables/useSession.js";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  compact: { type: Boolean, default: false },
  autoSave: { type: Boolean, default: false },
});

const { config, saveConfig, saveState } = useSession();
const { t } = useI18n();

const defaultHotkeys = {
  enabled: true,
  minimizeToTray: true,
  cycleMode: "Ctrl+Alt+M",
  togglePause: "Ctrl+Alt+P",
  retranslate: "Ctrl+Alt+R",
  fontScaleUp: "Ctrl+Alt+Up",
  fontScaleDown: "Ctrl+Alt+Down",
  toggleHooksMenu: "Ctrl+Alt+H",
};

const hotkeyForm = reactive({ ...defaultHotkeys });
const recordingField = shallowRef("");

const hotkeyFields = [
  { key: "toggleHooksMenu", labelKey: "hkToggleHooks" },
  { key: "togglePause", labelKey: "hkTogglePause" },
  { key: "cycleMode", labelKey: "hkCycleMode" },
  { key: "retranslate", labelKey: "hkRetranslate" },
  { key: "fontScaleUp", labelKey: "hkFontUp" },
  { key: "fontScaleDown", labelKey: "hkFontDown" },
];

watch(
  config,
  (cfg) => {
    if (cfg?.hotkeys) {
      Object.assign(hotkeyForm, { ...defaultHotkeys, ...cfg.hotkeys });
    }
  },
  { immediate: true },
);

const CODE_KEY_MAP = {
  ArrowUp: "Up",
  ArrowDown: "Down",
  ArrowLeft: "Left",
  ArrowRight: "Right",
  Equal: "=",
  Minus: "-",
  Space: "Space",
  Enter: "Enter",
  Backspace: "Backspace",
  Tab: "Tab",
  Insert: "Insert",
  Delete: "Delete",
  Home: "Home",
  End: "End",
  PageUp: "PageUp",
  PageDown: "PageDown",
  BracketLeft: "[",
  BracketRight: "]",
  Backslash: "\\",
  Semicolon: ";",
  Quote: "'",
  Comma: ",",
  Period: ".",
  Slash: "/",
  Backquote: "`",
};

function codeToKeyToken(code) {
  if (/^Key[A-Z]$/.test(code)) return code.slice(3);
  if (/^Digit[0-9]$/.test(code)) return code.slice(5);
  if (/^F([1-9]|1[0-9]|2[0-4])$/.test(code)) return code;
  return CODE_KEY_MAP[code] || "";
}

function onSave() {
  if (!config.value) return;
  saveConfig({
    ...config.value,
    hotkeys: {
      ...config.value.hotkeys,
      enabled: Boolean(hotkeyForm.enabled),
      minimizeToTray: Boolean(hotkeyForm.minimizeToTray),
      cycleMode: hotkeyForm.cycleMode.trim(),
      togglePause: hotkeyForm.togglePause.trim(),
      retranslate: hotkeyForm.retranslate.trim(),
      fontScaleUp: hotkeyForm.fontScaleUp.trim(),
      fontScaleDown: hotkeyForm.fontScaleDown.trim(),
      toggleHooksMenu: hotkeyForm.toggleHooksMenu.trim(),
    },
  });
}

function onToggleChange() {
  if (props.autoSave) onSave();
}

function clearField(field) {
  hotkeyForm[field] = "";
  if (props.autoSave) onSave();
}

function onHotkeyKeydown(field, e) {
  e.preventDefault();
  e.stopPropagation();
  if (e.key === "Escape") {
    recordingField.value = "";
    e.target?.blur?.();
    return;
  }
  const modCodes = new Set([
    "ControlLeft",
    "ControlRight",
    "AltLeft",
    "AltRight",
    "ShiftLeft",
    "ShiftRight",
    "MetaLeft",
    "MetaRight",
  ]);
  if (modCodes.has(e.code)) return;

  const token = codeToKeyToken(e.code);
  if (!token) return;

  const mods = [];
  if (e.ctrlKey) mods.push("Ctrl");
  if (e.altKey) mods.push("Alt");
  if (e.shiftKey) mods.push("Shift");
  if (e.metaKey) mods.push("Win");

  const isFn = /^F([1-9]|1[0-9]|2[0-4])$/.test(token);
  if (!mods.length && !isFn) return;

  hotkeyForm[field] = [...mods, token].join("+");
  recordingField.value = "";
  e.target?.blur?.();
  if (props.autoSave) onSave();
}

function resetHotkeys() {
  Object.assign(hotkeyForm, defaultHotkeys);
  if (props.autoSave) onSave();
}
</script>

<template>
  <div class="hk-panel" :class="{ compact }">
    <div v-if="!compact" class="panel-header">
      <h2 class="h2">{{ t("hkCardTitle") }}</h2>
      <p class="desc">{{ t("hkCardDesc") }}</p>
    </div>

    <div class="checks">
      <label class="chk">
        <input v-model="hotkeyForm.enabled" type="checkbox" @change="onToggleChange" />
        <span>{{ t("hkEnableMaster") }}</span>
      </label>
      <label class="chk">
        <input v-model="hotkeyForm.minimizeToTray" type="checkbox" @change="onToggleChange" />
        <span>{{ t("hkMinToTray") }}</span>
      </label>
    </div>

    <p class="desc">{{ t("hkRecorderHint") }}</p>

    <div class="hk-grid" :class="{ disabled: !hotkeyForm.enabled }">
      <div v-for="item in hotkeyFields" :key="item.key" class="field">
        <span class="lbl">{{ t(item.labelKey) }}</span>
        <div class="hk-input-wrap">
          <input
            type="text"
            readonly
            dir="ltr"
            class="input hk-input"
            :disabled="!hotkeyForm.enabled"
            :value="
              recordingField === item.key
                ? t('hkPressKeys')
                : hotkeyForm[item.key] || t('hkDisabledPlaceholder')
            "
            @focus="recordingField = item.key"
            @blur="recordingField = ''"
            @keydown="onHotkeyKeydown(item.key, $event)"
          />
          <Tooltip :text="t('hkClearTip')">
            <button
              type="button"
              class="hk-clear"
              :disabled="!hotkeyForm.enabled || !hotkeyForm[item.key]"
              @click="clearField(item.key)"
            >
              ×
            </button>
          </Tooltip>
        </div>
      </div>
    </div>

    <div class="hk-actions">
      <button class="btn secondary" type="button" @click="resetHotkeys">
        {{ t("hkResetDefaults") }}
      </button>
      <button v-if="!autoSave" class="btn primary" type="button" @click="onSave">
        {{ saveState === "saving" ? t("embSaving") : saveState === "saved" ? t("trSavedBtn") : t("embApplySave") }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.hk-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.hk-panel.compact {
  gap: 10px;
}
.panel-header {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.h2 {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}
.desc {
  margin: 0;
  font-size: 12px;
  color: var(--fg-muted);
}
.checks {
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.chk {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  cursor: pointer;
}
.chk input {
  accent-color: var(--accent);
}
.hk-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px 14px;
}
.hk-panel.compact .hk-grid {
  grid-template-columns: repeat(auto-fill, minmax(195px, 1fr));
  gap: 8px 12px;
}
.hk-grid.disabled {
  opacity: 0.55;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.lbl {
  font-size: 11px;
  color: var(--fg-muted);
}
.hk-input-wrap {
  display: flex;
  align-items: center;
  gap: 4px;
}
.input {
  height: 28px;
  padding: 0 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--fg);
  font: inherit;
  font-size: 12px;
}
.hk-input {
  flex: 1;
  min-width: 0;
  font-family: var(--mono);
  cursor: pointer;
}
.hk-input:focus {
  border-color: var(--accent);
  outline: none;
}
.hk-clear {
  height: 28px;
  width: 26px;
  border-radius: 5px;
  border: 1px solid var(--border);
  background: var(--paper);
  color: var(--fg-muted);
  cursor: pointer;
  font-size: 14px;
  line-height: 1;
}
.hk-clear:hover:not(:disabled) {
  color: #f87171;
  border-color: #f87171;
}
.hk-clear:disabled {
  opacity: 0.35;
  cursor: default;
}
.hk-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.btn.secondary {
  height: 28px;
  padding: 0 12px;
  background: var(--paper);
  color: var(--fg);
  border: 1px solid var(--border);
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
.btn.primary {
  height: 30px;
  padding: 0 16px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}
</style>
