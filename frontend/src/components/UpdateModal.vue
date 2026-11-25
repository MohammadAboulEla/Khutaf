<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import Icon from "./Icon.vue";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  status: { type: Object, required: true },
  attached: { type: Boolean, default: false },
});

const emit = defineEmits([
  "close",
  "download",
  "cancel-download",
  "install",
  "skip",
  "open-github",
  "restart-admin",
]);

const { t, lang } = useI18n();
const isRtl = computed(() => lang.value === "ar");

const autoInstallAfterDownload = ref(false);
const localError = ref("");

const state = computed(() => props.status?.state || "idle");
const busy = computed(() => ["downloading", "verifying", "applying"].includes(state.value));

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      localError.value = "";
    }
  },
);

watch(state, async (next) => {
  if (next === "ready" && autoInstallAfterDownload.value) {
    autoInstallAfterDownload.value = false;
    try {
      emit("install", true);
    } catch (e) {
      localError.value = String(e);
    }
  } else if (next === "error" || next === "available") {
    if (next === "available") {
      autoInstallAfterDownload.value = false;
    }
  }
});

function onKey(e) {
  if (e.key === "Escape" && props.open && state.value !== "applying") {
    emit("close");
  }
}
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

function formatBytes(bytes) {
  const n = Number(bytes || 0);
  if (!Number.isFinite(n) || n <= 0) return "0 MB";
  const mb = n / (1024 * 1024);
  if (mb >= 1) return `${mb.toFixed(1)} MB`;
  return `${Math.max(1, Math.round(n / 1024))} KB`;
}

const formattedDate = computed(() => {
  const raw = props.status?.publishedAt;
  if (!raw) return "";
  const d = new Date(raw);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleDateString(isRtl.value ? "ar-EG" : "en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
});

// Parse simple release notes lines safely without v-html.
const parsedNotes = computed(() => {
  const raw = (props.status?.releaseNotes || "").trim();
  if (!raw) return [];
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map((line) => {
      if (/^#{1,4}\s+/.test(line)) {
        return { kind: "heading", text: line.replace(/^#{1,4}\s+/, "").replace(/\*\*/g, "") };
      }
      if (/^[-*•]\s+/.test(line)) {
        return { kind: "bullet", text: line.replace(/^[-*•]\s+/, "").replace(/\*\*/g, "") };
      }
      return { kind: "text", text: line.replace(/\*\*/g, "") };
    });
});

function onUpdateNow() {
  localError.value = "";
  autoInstallAfterDownload.value = true;
  emit("download");
}

function onRestartAndInstall() {
  localError.value = "";
  emit("install", true);
}

function onCancelDownload() {
  autoInstallAfterDownload.value = false;
  emit("cancel-download");
}

function onSkip() {
  autoInstallAfterDownload.value = false;
  emit("skip", props.status?.latestVersion || "");
  emit("close");
}
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="state !== 'applying' && emit('close')">
        <div
          class="modal"
          role="dialog"
          aria-modal="true"
          :aria-label="t('updModalTitle')"
          :dir="isRtl ? 'rtl' : 'ltr'"
        >
          <header class="head">
            <div class="title-wrap">
              <span class="title-ico">
                <Icon name="Sparkles" :size="15" />
              </span>
              <span class="title">{{ t("updModalTitle") }}</span>
            </div>
            <button
              class="close"
              type="button"
              :disabled="state === 'applying'"
              @click="emit('close')"
              :aria-label="t('updBtnLater')"
            >
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <div class="version-card">
              <div class="ver-col">
                <span class="ver-lbl">{{ t("updCurrentLbl") }}</span>
                <span class="ver-num" dir="ltr">v{{ status.currentVersion }}</span>
              </div>
              <span class="ver-arrow" dir="ltr">→</span>
              <div class="ver-col">
                <span class="ver-lbl">{{ t("updNewLbl") }}</span>
                <div class="ver-new-row">
                  <span class="ver-num new" dir="ltr">v{{ status.latestVersion }}</span>
                  <span v-if="status.prerelease" class="beta-tag">{{ t("updPrerelease") }}</span>
                </div>
              </div>
              <div class="ver-meta">
                <span v-if="formattedDate">{{ t("updPublishedLbl", { date: formattedDate }) }}</span>
                <span v-if="status.assetSize">{{ t("updSizeLbl", { size: formatBytes(status.assetSize) }) }}</span>
              </div>
            </div>

            <div v-if="attached" class="notice warn">
              <Icon name="AlertTri" :size="15" />
              <span>{{ t("updAttachedWarn") }}</span>
            </div>

            <div v-if="status.writable === false" class="notice danger">
              <Icon name="Lock" :size="15" />
              <div class="notice-col">
                <span>{{ t("updReadOnlyWarn") }}</span>
                <div class="notice-actions">
                  <button class="mini-btn" type="button" @click="emit('restart-admin')">
                    {{ t("updBtnAdmin") }}
                  </button>
                  <button class="mini-btn" type="button" @click="emit('open-github', status.releaseUrl)">
                    <Icon name="ExternalLink" :size="12" />
                    <span>{{ t("updBtnGithub") }}</span>
                  </button>
                </div>
              </div>
            </div>

            <div v-if="state === 'downloading' || state === 'verifying' || state === 'applying'" class="progress-box">
              <div class="progress-head">
                <span v-if="state === 'downloading'">
                  {{
                    t("updDownloading", {
                      downloaded: formatBytes(status.downloadedBytes),
                      total: formatBytes(status.totalBytes || status.assetSize),
                    })
                  }}
                </span>
                <span v-else-if="state === 'verifying'">{{ t("updVerifying") }}</span>
                <span v-else>{{ t("updApplying") }}</span>
                <span class="pct" dir="ltr">{{ status.progress }}%</span>
              </div>
              <div class="bar-track">
                <div class="bar-fill" :style="{ width: `${Math.min(100, Math.max(0, status.progress || 0))}%` }" />
              </div>
            </div>

            <div v-else-if="state === 'ready'" class="notice success">
              <Icon name="CheckCircle" :size="15" />
              <span>{{ t("updReadyBanner") }}</span>
            </div>

            <div v-if="state === 'error' || localError" class="notice danger">
              <Icon name="AlertCircle" :size="15" />
              <div class="notice-col">
                <span>{{ localError || status.error }}</span>
              </div>
            </div>

            <div class="notes-section">
              <div class="notes-title">{{ t("updNotesTitle") }}</div>
              <div class="notes-box" dir="auto">
                <template v-if="parsedNotes.length">
                  <template v-for="(item, idx) in parsedNotes" :key="idx">
                    <div v-if="item.kind === 'heading'" class="note-heading">{{ item.text }}</div>
                    <div v-else-if="item.kind === 'bullet'" class="note-bullet">
                      <span class="bullet-dot">•</span>
                      <span>{{ item.text }}</span>
                    </div>
                    <p v-else class="note-p">{{ item.text }}</p>
                  </template>
                </template>
                <div v-else class="notes-empty">{{ t("updNotesEmpty") }}</div>
              </div>
            </div>
          </div>

          <footer class="foot">
            <div class="foot-secondary">
              <button
                v-if="state !== 'downloading' && state !== 'verifying' && state !== 'applying'"
                class="btn ghost"
                type="button"
                @click="onSkip"
              >
                {{ t("updBtnSkip") }}
              </button>
              <button
                class="btn ghost"
                type="button"
                @click="emit('open-github', status.releaseUrl)"
              >
                <Icon name="ExternalLink" :size="13" />
                <span>{{ t("updBtnGithub") }}</span>
              </button>
            </div>

            <div class="foot-primary">
              <button
                v-if="state === 'downloading'"
                class="btn secondary"
                type="button"
                @click="onCancelDownload"
              >
                {{ t("updBtnCancel") }}
              </button>
              <button
                v-else-if="state !== 'applying'"
                class="btn secondary"
                type="button"
                @click="emit('close')"
              >
                {{ t("updBtnLater") }}
              </button>

              <button
                v-if="state === 'ready'"
                class="btn primary ok"
                type="button"
                @click="onRestartAndInstall"
              >
                <Icon name="RefreshCw" :size="13" />
                <span>{{ t("updBtnRestart") }}</span>
              </button>
              <button
                v-else-if="!busy && status.writable !== false"
                class="btn primary"
                type="button"
                @click="onUpdateNow"
              >
                <Icon name="Download" :size="13" />
                <span>{{ state === "error" ? t("updBtnRetry") : t("updBtnNow") }}</span>
              </button>
            </div>
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
  background: rgba(0, 0, 0, 0.5);
  display: grid;
  place-items: center;
  z-index: 1050;
  padding: 24px;
}
.modal {
  display: grid;
  grid-template-rows: auto 1fr auto;
  width: min(560px, 100%);
  max-height: min(620px, 90vh);
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border-input, var(--border));
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
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
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
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
.close:disabled {
  opacity: 0.4;
  cursor: default;
}

.body {
  overflow-y: auto;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.version-card {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  padding: 12px 14px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.ver-col {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.ver-lbl {
  font-size: 11px;
  color: var(--fg-muted);
}
.ver-num {
  font-family: var(--mono);
  font-size: 14px;
  font-weight: 600;
  color: var(--fg);
}
.ver-num.new {
  color: var(--accent);
}
.ver-arrow {
  color: var(--fg-faint);
  font-size: 15px;
}
.ver-new-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.beta-tag {
  font-size: 10px;
  font-weight: 600;
  padding: 1px 6px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--accent) 16%, transparent);
  color: var(--accent);
  border: 1px solid color-mix(in oklab, var(--accent) 35%, var(--border));
}
.ver-meta {
  margin-inline-start: auto;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  font-size: 11.5px;
  color: var(--fg-muted);
}

.notice {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 10px 12px;
  border-radius: 8px;
  font-size: var(--fs-small);
  line-height: 1.5;
  border: 1px solid var(--border);
}
.notice.warn {
  background: color-mix(in oklab, #f59e0b 12%, var(--bg-pill));
  border-color: color-mix(in oklab, #f59e0b 40%, var(--border));
  color: var(--fg);
}
.notice.warn > svg {
  color: #f59e0b;
  flex-shrink: 0;
  margin-top: 2px;
}
.notice.danger {
  background: color-mix(in oklab, #ef4444 12%, var(--bg-pill));
  border-color: color-mix(in oklab, #ef4444 40%, var(--border));
  color: var(--fg);
}
.notice.danger > svg {
  color: #ef4444;
  flex-shrink: 0;
  margin-top: 2px;
}
.notice.success {
  background: color-mix(in oklab, #22c55e 12%, var(--bg-pill));
  border-color: color-mix(in oklab, #22c55e 40%, var(--border));
  color: var(--fg);
}
.notice.success > svg {
  color: #22c55e;
  flex-shrink: 0;
  margin-top: 2px;
}
.notice-col {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-width: 0;
}
.notice-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.mini-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 26px;
  padding: 0 10px;
  border-radius: 5px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--paper);
  color: var(--fg);
  font: inherit;
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
}
.mini-btn:hover {
  background: var(--bg-hover);
}

.progress-box {
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.progress-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: var(--fs-small);
}
.pct {
  font-family: var(--mono);
  font-weight: 600;
  color: var(--accent);
}
.bar-track {
  height: 7px;
  border-radius: 999px;
  background: var(--bg-pill);
  overflow: hidden;
}
.bar-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 999px;
  transition: width 0.15s ease;
}

.notes-section {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.notes-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--fg-muted);
}
.notes-box {
  max-height: 220px;
  overflow-y: auto;
  padding: 12px;
  border-radius: 8px;
  background: var(--paper);
  border: 1px solid var(--border);
  font-size: 12.5px;
  line-height: 1.55;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.note-heading {
  font-weight: 600;
  color: var(--fg);
  margin-top: 4px;
}
.note-heading:first-child {
  margin-top: 0;
}
.note-bullet {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  color: var(--fg);
}
.bullet-dot {
  color: var(--accent);
  font-weight: 700;
}
.note-p {
  margin: 0;
  color: var(--fg-muted);
}
.notes-empty {
  color: var(--fg-faint);
}

.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}
.foot-secondary,
.foot-primary {
  display: flex;
  align-items: center;
  gap: 6px;
}
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border-radius: 6px;
  font: inherit;
  font-size: var(--fs-small);
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
}
.btn.ghost {
  background: transparent;
  color: var(--fg-muted);
}
.btn.ghost:hover {
  background: color-mix(in oklab, var(--fg) 8%, transparent);
  color: var(--fg);
}
.btn.secondary {
  background: var(--paper);
  border-color: var(--border-input, var(--border));
  color: var(--fg);
}
.btn.secondary:hover {
  background: var(--bg-hover);
}
.btn.primary {
  background: var(--accent);
  color: #fff;
}
.btn.primary.ok {
  background: #16a34a;
}
.btn.primary:hover {
  opacity: 0.92;
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
