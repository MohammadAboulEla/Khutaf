<script setup>
import { onMounted, onUnmounted } from "vue";
import Icon from "./Icon.vue";

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: "Khutaf" },
  version: { type: String, default: "0.1.0" },
});

const emit = defineEmits(["close"]);

const onKey = (e) => {
  if (e.key === "Escape" && props.open) emit("close");
};
onMounted(() => window.addEventListener("keydown", onKey));
onUnmounted(() => window.removeEventListener("keydown", onKey));

const sections = [
  {
    heading: "About Khutaf",
    body: "Khutaf (خُطّاف) is a real-time in-game Arabic text hook and embedded translator for Windows PC games. It attaches to running games, intercepts dialogue directly from memory, translates it, and injects properly shaped RTL Arabic back into the game UI.",
  },
  {
    heading: "Arabic Shaping & BiDi Engine",
    body: "Built-in contextual shaping (Presentation Forms + Lam-Alef ligatures), logical line wrapping before reversal, bracket mirroring, and LTR run preservation so <tags>, {vars}, and numbers stay intact inside LTR-only game engines.",
  },
  {
    heading: "Unity & Native Hooks",
    body: "Supports Unity Mono and IL2CPP TextMeshPro/UI.Text dynamic OS font loading and fallback SDF asset bundles via custom Rust khutaf-hook64.",
  },
  {
    heading: "Translators & Pipeline",
    body: "Includes free Bing web translation and OpenAI-compatible LLM endpoints (OpenAI, Gemini, DeepSeek, Groq, OpenRouter, Ollama, llama.cpp), pre/post regex & literal rules, request deduplication, and live performance profiling in the Debug tab.",
  },
  {
    heading: "Shortcuts & Persistence",
    body: "Press Esc to close modals. Saved game profiles and embed hooks are stored in userconfig/games.json, settings in userconfig/config.json, and UI theme/sidebar preferences persist automatically.",
  },
];
</script>

<template>
  <Teleport to="body">
    <Transition name="fade">
      <div v-if="open" class="overlay" @click.self="$emit('close')">
        <div class="modal" role="dialog" aria-modal="true" :aria-label="`About ${title}`">
          <header class="head">
            <div class="title">About {{ title }}</div>
            <button class="close" type="button" @click="$emit('close')" aria-label="Close">
              <Icon name="X" :size="14" />
            </button>
          </header>

          <div class="body">
            <div class="hero">
              <div class="mark">Kh</div>
              <div>
                <div class="name">{{ title }} · خُطّاف</div>
                <div class="ver">version {{ version }} · Embedded Arabic Game Hook</div>
              </div>
            </div>

            <section v-for="s in sections" :key="s.heading">
              <h3>{{ s.heading }}</h3>
              <p>{{ s.body }}</p>
            </section>
          </div>

          <footer class="foot">
            <span class="muted">© 2026</span>
            <button class="btn" type="button" @click="$emit('close')">Close</button>
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
  grid-template-rows: auto 1fr auto;
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

.body {
  overflow-y: auto;
  padding: 16px 18px;
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

section { margin-top: 14px; }
h3 {
  margin: 0 0 4px;
  font-size: 13px;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--fg-muted);
  font-family: var(--mono);
}
p { margin: 0; line-height: 1.55; color: var(--fg); }

.foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 14px;
  border-top: 1px solid var(--border);
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
