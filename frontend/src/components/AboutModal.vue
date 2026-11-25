<script setup>
import { computed, onMounted, onUnmounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: "Khutaf" },
  version: { type: String, default: "0.1.5-beta" },
  updateStatus: { type: Object, default: () => ({ state: "idle" }) },
  autoCheckUpdates: { type: Boolean, default: true },
});

const emit = defineEmits(["close", "check-updates", "open-update", "toggle-auto-check"]);
const { lang: appLang, t } = useI18n();

const STORAGE_KEY = "khutaf.aboutLang";
const readInitialLang = () => {
  try {
    const saved = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
    if (saved === "ar" || saved === "en") return saved;
  } catch {
    // ignore
  }
  return appLang.value === "en" ? "en" : "ar";
};

const lang = shallowRef(readInitialLang());
watch(appLang, (next) => {
  if (next === "ar" || next === "en") lang.value = next;
});
const isRtl = computed(() => lang.value === "ar");

function setLang(next) {
  lang.value = next;
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Storage may be unavailable in restricted webviews.
  }
}

const onKey = (e) => {
  if (e.key === "Escape" && props.open) emit("close");
};
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

const content = computed(() => ({
  en: {
    modalTitle: `About ${props.title}`,
    heroName: `${props.title} · خُطّاف`,
    versionLabel: `v${props.version}`,
    subtitle: "Live Arabic translation inside your games",
    intro:
      "Khutaf lets you play PC games in Arabic by translating dialogue and menus live right inside your game, without modifying any original game files.",
    features: [
      {
        icon: "Zap",
        title: "Fast & Smooth",
        desc: "Translates dialogue and menus in real time without freezing your game.",
      },
      {
        icon: "ALargeSmall",
        title: "Clear Arabic Display",
        desc: "Properly shaped right-to-left Arabic text with customizable fonts, size, and display styles.",
      },
      {
        icon: "Globe",
        title: "Flexible Translators",
        desc: "Works out of the box with free translators, plus optional AI and offline translation.",
      },
      {
        icon: "Keyboard",
        title: "In-Game Controls",
        desc: "Keyboard shortcuts to pause or resize text on the fly, with per-game settings saved automatically.",
      },
    ],
    notes: [
      {
        icon: "Gamepad",
        caution: false,
        text: "Supported games: 64-bit Unity games on Windows, with more game engines planned in future updates.",
      },
      {
        icon: "Shield",
        caution: true,
        text: "For offline single-player games only — do not use with online multiplayer or anti-cheat protected games.",
      },
    ],
    closeLabel: "Close",
    footerNote: "© 2026 Mohammad AboulEla · GPL-3.0",
  },
  ar: {
    modalTitle: "حول خُطّاف",
    heroName: `خُطّاف · ${props.title}`,
    versionLabel: `الإصدار ${props.version}`,
    subtitle: "ترجمة عربية فورية داخل ألعابك",
    intro:
      "يساعدك «خُطّاف» على لعب ألعاب الكمبيوتر باللغة العربية من خلال ترجمة الحوارات والقوائم مباشرةً داخل اللعبة أثناء اللعب، دون تعديل ملفات اللعبة الأصلية.",
    features: [
      {
        icon: "Zap",
        title: "ترجمة فورية وسلسة",
        desc: "تظهر الترجمة تلقائياً أثناء اللعب بدون تجميد اللعبة أو التأثير على سلاستها.",
      },
      {
        icon: "ALargeSmall",
        title: "عرض عربي واضح وسليم",
        desc: "حروف عربية متصلة ومقروءة مع إمكانية تغيير الخط وحجم النص ونمط العرض.",
      },
      {
        icon: "Globe",
        title: "خيارات ترجمة متعددة",
        desc: "يعمل مباشرةً بمترجمات مجانية بدون إعداد، مع دعم اختياري للذكاء الاصطناعي والترجمة بدون إنترنت.",
      },
      {
        icon: "Keyboard",
        title: "تحكّم سريع أثناء اللعب",
        desc: "اختصارات لوحة مفاتيح للإيقاف المؤقت وتعديل الخط، وحفظ تلقائي لإعدادات كل لعبة.",
      },
    ],
    notes: [
      {
        icon: "Gamepad",
        caution: false,
        text: "الألعاب المدعومة حالياً: ألعاب Unity (64-bit) على ويندوز، مع خطة لدعم محركات أخرى لاحقاً.",
      },
      {
        icon: "Shield",
        caution: true,
        text: "مخصص للألعاب الفردية (Offline) فقط — تجنّب استخدامه مع الألعاب الجماعية أو المحمية بأنظمة مكافحة الغش.",
      },
    ],
    closeLabel: "إغلاق",
    footerNote: "© 2026 Mohammad AboulEla · GPL-3.0",
  },
}));

const active = computed(() => content.value[lang.value]);

const updateSummary = computed(() => {
  const st = props.updateStatus?.state || "idle";
  if (st === "checking") return t("updAboutChecking");
  if (st === "available" || st === "downloading" || st === "verifying") {
    return t("updAboutAvailable", { version: props.updateStatus?.latestVersion || "" });
  }
  if (st === "ready") {
    return t("updAboutReady", { version: props.updateStatus?.latestVersion || "" });
  }
  if (st === "up_to_date") return t("updAboutUpToDate");
  if (st === "error") return props.updateStatus?.error || t("updAboutIdle");
  return t("updAboutIdle");
});

const hasActionableUpdate = computed(() =>
  ["available", "downloading", "verifying", "ready"].includes(props.updateStatus?.state),
);
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="$emit('close')">
        <div
          class="modal"
          role="dialog"
          aria-modal="true"
          :aria-label="active.modalTitle"
        >
          <header class="head" :dir="isRtl ? 'rtl' : 'ltr'">
            <div class="title">{{ active.modalTitle }}</div>
            <button
              class="close"
              type="button"
              @click="$emit('close')"
              :aria-label="active.closeLabel"
            >
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="tabs-bar" role="tablist" aria-label="Language">
            <button
              type="button"
              role="tab"
              class="tab"
              :class="{ active: lang === 'en' }"
              :aria-selected="lang === 'en'"
              @click="setLang('en')"
            >
              English
            </button>
            <button
              type="button"
              role="tab"
              class="tab"
              :class="{ active: lang === 'ar' }"
              :aria-selected="lang === 'ar'"
              @click="setLang('ar')"
            >
              العربية
            </button>
          </div>

          <div class="body" :dir="isRtl ? 'rtl' : 'ltr'">
            <div class="hero">
              <div class="mark" dir="ltr">Kh</div>
              <div class="hero-meta">
                <div class="hero-top">
                  <span class="name">{{ active.heroName }}</span>
                  <span class="ver-badge">{{ active.versionLabel }}</span>
                </div>
                <div class="ver">{{ active.subtitle }}</div>
              </div>
            </div>

            <p class="intro">{{ active.intro }}</p>

            <div class="features-grid">
              <div v-for="item in active.features" :key="item.title" class="feature-card">
                <div class="feature-head">
                  <span class="feature-ico">
                    <Icon :name="item.icon" :size="15" />
                  </span>
                  <span class="feature-title">{{ item.title }}</span>
                </div>
                <p class="feature-desc">{{ item.desc }}</p>
              </div>
            </div>

            <div class="notes">
              <div
                v-for="note in active.notes"
                :key="note.text"
                class="note-row"
                :class="{ caution: note.caution }"
              >
                <span class="note-ico">
                  <Icon :name="note.icon" :size="15" />
                </span>
                <span class="note-text">{{ note.text }}</span>
              </div>
            </div>

            <div class="update-card">
              <div class="update-top">
                <div class="update-info">
                  <span class="update-title">{{ t("updAboutSection") }}</span>
                  <span class="update-sub" :class="{ avail: hasActionableUpdate }">
                    {{ updateSummary }}
                  </span>
                </div>
                <button
                  v-if="hasActionableUpdate"
                  class="upd-btn accent"
                  type="button"
                  @click="emit('open-update')"
                >
                  <Icon name="Download" :size="13" />
                  <span>{{ t("updAboutViewBtn") }}</span>
                </button>
                <button
                  v-else
                  class="upd-btn"
                  type="button"
                  :disabled="updateStatus?.state === 'checking'"
                  @click="emit('check-updates')"
                >
                  <Icon name="RefreshCw" :size="13" />
                  <span>{{ t("updAboutCheckBtn") }}</span>
                </button>
              </div>
              <label class="upd-check">
                <input
                  type="checkbox"
                  :checked="autoCheckUpdates"
                  @change="emit('toggle-auto-check', $event.target.checked)"
                />
                <span>{{ t("updAboutAutoCheck") }}</span>
              </label>
            </div>
          </div>

          <footer class="foot" :dir="isRtl ? 'rtl' : 'ltr'">
            <span class="muted" dir="ltr">{{ active.footerNote }}</span>
            <button class="btn" type="button" @click="$emit('close')">{{ active.closeLabel }}</button>
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
  background: rgba(0, 0, 0, 0.45);
  display: grid;
  place-items: center;
  z-index: 1000;
  padding: 24px;
}
.modal {
  display: grid;
  grid-template-rows: auto auto 1fr auto;
  width: min(560px, 100%);
  max-height: min(640px, 90vh);
  background: var(--bg-elev);
  color: var(--fg);
  border: 1px solid var(--border-input, var(--border));
  border-radius: 10px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.35);
  overflow: hidden;
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
}
.head[dir="rtl"] {
  font-family: var(--arabic);
}
.title { font-weight: 600; }
.close {
  width: 26px; height: 26px;
  display: grid; place-items: center;
  background: transparent;
  border: none;
  border-radius: 5px;
  color: var(--fg);
  cursor: pointer;
}
.close:hover { background: color-mix(in oklab, var(--fg) 10%, transparent); }

.tabs-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-pill);
}
.tab {
  height: 26px;
  padding: 0 12px;
  background: transparent;
  color: var(--fg-muted);
  border: 1px solid transparent;
  border-radius: 5px;
  font: inherit;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.tab:hover {
  color: var(--fg);
}
.tab.active {
  background: var(--paper);
  color: var(--fg);
  border-color: var(--border-input, var(--border));
}

.body {
  overflow-y: auto;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.body[dir="rtl"] {
  font-family: var(--arabic);
}
.hero {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 14px;
  border-bottom: 1px solid var(--border);
}
.mark {
  width: 44px; height: 44px;
  flex-shrink: 0;
  border-radius: 8px;
  background: var(--accent);
  color: #fff;
  display: grid; place-items: center;
  font-weight: 700;
  font-size: 18px;
}
.hero-meta {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.hero-top {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.name {
  font-weight: 600;
  font-size: 16px;
}
.ver-badge {
  display: inline-flex;
  align-items: center;
  height: 20px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--bg-pill);
  border: 1px solid var(--border);
  color: var(--fg-muted);
  font-family: var(--mono);
  font-size: 11px;
  font-weight: 600;
}
.body[dir="rtl"] .ver-badge {
  font-family: var(--arabic);
}
.ver {
  color: var(--fg-muted);
  font-size: 13px;
}

.intro {
  margin: 0;
  line-height: 1.6;
  color: var(--fg);
  font-size: 13.5px;
}
.body[dir="rtl"] .intro {
  line-height: 1.75;
}

.features-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}
@media (max-width: 520px) {
  .features-grid {
    grid-template-columns: 1fr;
  }
}
.feature-card {
  padding: 11px 12px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.feature-head {
  display: flex;
  align-items: center;
  gap: 7px;
}
.feature-ico {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 6px;
  display: grid;
  place-items: center;
  background: color-mix(in oklab, var(--accent) 14%, transparent);
  color: var(--accent);
}
.feature-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
}
.feature-desc {
  margin: 0;
  font-size: var(--fs-small);
  line-height: 1.5;
  color: var(--fg-muted);
}
.body[dir="rtl"] .feature-desc {
  line-height: 1.65;
}

.notes {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.note-row {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  padding: 9px 11px;
  border-radius: 8px;
  background: var(--bg-pill);
  border: 1px solid var(--border);
  font-size: var(--fs-small);
  line-height: 1.5;
  color: var(--fg-muted);
}
.body[dir="rtl"] .note-row {
  line-height: 1.65;
}
.note-row.caution {
  background: color-mix(in oklab, var(--accent) 10%, var(--bg-pill));
  border-color: color-mix(in oklab, var(--accent) 35%, var(--border));
  color: var(--fg);
}
.note-ico {
  flex-shrink: 0;
  margin-top: 2px;
  color: var(--fg-muted);
  display: grid;
  place-items: center;
}
.note-row.caution .note-ico {
  color: var(--accent);
}

.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
}
.foot[dir="rtl"] .btn {
  font-family: var(--arabic);
}
.muted { color: var(--fg-faint); font-size: var(--fs-small); font-family: var(--mono); }
.btn {
  height: 28px;
  padding: 0 14px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 6px;
  font: inherit;
  font-size: var(--fs-small);
  font-weight: 500;
  cursor: pointer;
}
.btn:hover { opacity: 0.9; }

.update-card {
  padding: 10px 12px;
  background: var(--paper);
  border: 1px solid var(--border);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.update-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.update-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.update-title {
  font-size: 12.5px;
  font-weight: 600;
  color: var(--fg);
}
.update-sub {
  font-size: var(--fs-small);
  color: var(--fg-muted);
}
.update-sub.avail {
  color: var(--accent);
  font-weight: 500;
}
.upd-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 28px;
  padding: 0 11px;
  border-radius: 6px;
  border: 1px solid var(--border-input, var(--border));
  background: var(--bg-elev);
  color: var(--fg);
  font: inherit;
  font-size: var(--fs-small);
  font-weight: 500;
  cursor: pointer;
}
.upd-btn:hover:not(:disabled) {
  background: var(--bg-hover);
}
.upd-btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.upd-btn.accent {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.upd-check {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: var(--fs-small);
  color: var(--fg-muted);
  cursor: pointer;
}
.upd-check input {
  accent-color: var(--accent);
}

.fade-enter-active, .fade-leave-active { transition: opacity 0.12s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
