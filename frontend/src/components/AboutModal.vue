<script setup>
import { computed, onMounted, onUnmounted, shallowRef, watch } from "vue";
import Icon from "./Icon.vue";
import { useI18n } from "../composables/useI18n.js";

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: "Khutaf" },
  version: { type: String, default: "0.1.3-beta" },
});

const emit = defineEmits(["close"]);
const { lang: appLang } = useI18n();

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
    subtitle: `version ${props.version} · Live Arabic Unity Game Hook`,
    closeLabel: "Close",
    footerNote: "© 2026 Mohammad AboulEla · GPL-3.0",
    sections: [
      {
        heading: "About Khutaf",
        body: "Khutaf (خُطّاف) is a real-time in-game Arabic text hook and embedded translator for 64-bit Unity (Mono & IL2CPP) PC games on Windows. It attaches to running games, intercepts text directly from memory, translates it, and injects shaped RTL Arabic back into the UI without modifying game files.",
      },
      {
        heading: "Non-Blocking Hooking & Unity Support",
        body: "Powered by custom Rust khutaf-hook64.dll (PROTO_VERSION = 2), async mode translates off the Unity main thread and writes back on frame ticks (Canvas::SendWillRenderCanvases / Camera::FireOnPreCull) with zero gameplay freeze. Configure the pending placeholder (Original default, \"...\", or custom text) with auto-restore on timeout/error, zero-alloc UTF-16 pre-filtering, circuit breaker recovery, and per-call-site #ctx Embed toggles in userconfig/games.json.",
      },
      {
        heading: "Arabic Shaping, BiDi & Unity Fonts",
        body: "Built-in contextual shaping (Presentation Forms + Lam-Alef ligatures + Tashkeel), logical word-boundary wrapping before reversal, bracket mirroring, and preservation of Latin words, numbers, [vars], {vars}, CamelCase tokens, and <color=...> rich-text tags. Supports dynamic OS fonts (Tahoma default) and one-click SHA-256-verified TMP_Font_AssetBundles downloads in Embed Settings.",
      },
      {
        heading: "Translators, Pipeline & 3-Tier Cache",
        body: "Includes free Bing web translation by default plus 11 traditional web translators, OpenAI-compatible LLMs (OpenAI, Gemini, DeepSeek, Groq, OpenRouter, Ollama, LM Studio), and a built-in llama.cpp runner with offline templates (HY-MT2, TranslateGemma, NLLB-Instruct). Features pre/post literal & regex rules, global/per-game glossaries, and a 3-tier SQLite cache with a manual cache editor.",
      },
      {
        heading: "Security, Anti-Cheat & Diagnostics",
        body: "Intended for offline single-player games only, with built-in anti-cheat process detection warnings. LLM API keys are encrypted at rest via Windows DPAPI, IPC named pipes enforce owner-only ACLs with per-session random handshakes, and the Debug tab provides live telemetry with redacted JSONL diagnostic bundle export.",
      },
      {
        heading: "Shortcuts, Persistence & License",
        body: "Press Esc to close modals. Settings persist in userconfig/config.json, per-game hooks in userconfig/games.json, and translations in SQLite. Licensed under GNU GPL-3.0 (Copyright © 2026 Mohammad AboulEla).",
      },
    ],
  },
  ar: {
    modalTitle: "حول خُطّاف",
    heroName: `خُطّاف · ${props.title}`,
    subtitle: `الإصدار ${props.version} · حقن وترجمة فورية لألعاب Unity`,
    closeLabel: "إغلاق",
    footerNote: "© 2026 Mohammad AboulEla · GPL-3.0",
    sections: [
      {
        heading: "حول خُطّاف",
        body: "برنامج «خُطّاف» (Khutaf) هو أداة للترجمة الفورية والحقن النصي باللغة العربية لألعاب الكمبيوتر المبنية بمحرك Unity (64-bit Mono وIL2CPP) على نظام ويندوز. يتصل باللعبة أثناء تشغيلها لالتقاط النصوص من الذاكرة، وترجمتها، وحقن العربية المشكّلة مباشرة داخل واجهة اللعبة بدون تعديل ملفاتها.",
      },
      {
        heading: "الحقن غير الحاجز ودعم Unity",
        body: "يعتمد على مكتبة Rust المخصصة khutaf-hook64.dll (بروتوكول PROTO_VERSION = 2)، حيث تُنفّذ الترجمة غير الحاجزة (Async) خارج المسار الرئيسي للعبة وتُكتب عند تحديث الإطارات (Canvas::SendWillRenderCanvases / Camera::FireOnPreCull) دون تجميد اللعب. يمكنك تخصيص نص الانتظار (الأصلي افتراضياً، أو \"...\"، أو نص مخصص) مع الاستعادة التلقائية عند الفشل، والترشيح الأولي السريع لنصوص UTF-16، وقاطع الدائرة، وحفظ تفعيل الـ Embed لكل #ctx في userconfig/games.json.",
      },
      {
        heading: "تشكيل العربية والـ BiDi وخطوط Unity",
        body: "محرك مدمج للتشكيل السياقي (الأشكال التقديمية ودمج اللام ألف والحفاظ على التشكيل)، والتفاف الأسطر المنطقي على حدود الكلمات قبل العكس، وعكس الأقواس، مع الحفاظ على الكلمات الإنجليزية والأرقام والمتغيرات [vars] و{vars} ومعرّفات CamelCase ووسوم <color=...> سليمة. يدعم خطوط النظام الديناميكية (Tahoma افتراضياً) وتحميل حزم TMP_Font_AssetBundles المتحقق منها بـ SHA-256 بضغطة زر من Embed Settings.",
      },
      {
        heading: "المترجمات ومعالجة النصوص والكاش",
        body: "يوفّر ترجمة Bing المجانية افتراضياً بالإضافة إلى 11 مترجماً تقليدياً، ودعم نماذج LLM المتوافقة مع OpenAI (OpenAI وGemini وDeepSeek وGroq وOpenRouter وOllama وLM Studio)، ومُشغّل llama.cpp مدمج بقوالب محلية (HY-MT2 وTranslateGemma وNLLB-Instruct). يشمل قواعد نصية وRegex قبل الترجمة وبعدها، وقاموس مصطلحات عام ولكل لعبة، وكاش SQLite ثلاثي المستويات مع محرر يدوي.",
      },
      {
        heading: "الأمان ومكافحة الغش والتشخيص",
        body: "مخصّص للألعاب الفردية (Offline) فقط مع تنبيهات مدمجة عند اكتشاف عمليات مكافحة الغش. تُشفَّر مفاتيح API محلياً عبر Windows DPAPI، وتُقيَّد الأنابيب المسماة بصلاحيات المالك فقط مع مصافحة عشوائية لكل جلسة، كما يعرض تبويب Debug قياسات الأداء الحية مع تصدير تقارير تشخيص JSONL مجهّلة البيانات الحساسة.",
      },
      {
        heading: "الاختصارات والحفظ والرخصة",
        body: "اضغط Esc لإغلاق النوافذ المنبثقة. تُحفظ الإعدادات في userconfig/config.json، وملفات الألعاب والـ Hooks في userconfig/games.json، والترجمات في SQLite. البرنامج مرخّص تحت رخصة GNU GPL-3.0 (Copyright © 2026 Mohammad AboulEla).",
      },
    ],
  },
}));

const active = computed(() => content.value[lang.value]);
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
              <div>
                <div class="name">{{ active.heroName }}</div>
                <div class="ver" :dir="isRtl ? 'rtl' : 'ltr'">{{ active.subtitle }}</div>
              </div>
            </div>

            <section v-for="s in active.sections" :key="s.heading">
              <h3>{{ s.heading }}</h3>
              <p>{{ s.body }}</p>
            </section>
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
  width: min(600px, 100%);
  max-height: min(680px, 90vh);
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
}
.body[dir="rtl"] {
  font-family: var(--arabic);
}
.hero {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 14px;
  margin-bottom: 6px;
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
.name { font-weight: 600; font-size: 16px; }
.ver {
  color: var(--fg-faint);
  font-family: var(--mono);
  font-size: var(--fs-small);
}
.body[dir="rtl"] .ver {
  font-family: var(--arabic);
}

section { margin-top: 14px; }
h3 {
  margin: 0 0 4px;
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-muted);
  font-family: var(--mono);
}
.body[dir="rtl"] h3 {
  font-family: var(--arabic);
  text-transform: none;
  letter-spacing: 0;
  font-size: 14px;
  font-weight: 700;
}
p { margin: 0; line-height: 1.55; color: var(--fg); }
.body[dir="rtl"] p { line-height: 1.7; }

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

.fade-enter-active, .fade-leave-active { transition: opacity 0.12s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
