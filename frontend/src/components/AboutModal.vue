<script setup>
import { onMounted, onUnmounted } from "vue";
import Icon from "./Icon.vue";

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: "Asar" },
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
    heading: "About this app",
    body: "Asar is a minimal Wails + Vue 3 desktop template. It ships a thin app shell — header, sidebar, main, footer — and stays out of your way. Use it as a starting point for any tool that needs to feel native on Windows while reusing the Vue ecosystem.",
  },
  {
    heading: "Design principles",
    body: "Tokens over hardcoded colors. Scoped styles per component. One source of truth for state, derived everywhere else. Compose with small files instead of growing a single mega-component. The shell does layout; everything else is opt-in.",
  },
  {
    heading: "Stack",
    body: "Vue 3 with the Composition API and <script setup>. Wails v2 for the native window and Go bridge. Vite for HMR. No CSS framework — just CSS variables and grid/flex. No global state library yet; composables hold cross-component UI state.",
  },
  {
    heading: "Keyboard shortcuts",
    body: "Esc closes any open modal. Window controls live in the header (minimize, maximize, close). The theme and sidebar-side buttons persist your choice to localStorage so the app reopens the way you left it.",
  },
  {
    heading: "Acknowledgements",
    body: "Lucide icons. The Vue and Wails teams for their respective tooling. The open-source community for endless examples to learn from. And every late-night session that turned 'just a quick prototype' into a real piece of software.",
  },
  {
    heading: "License",
    body: "MIT. Use it, fork it, sell it, ignore the author. A link back is appreciated but never required. No warranty — if this app eats your data, that's on you, not on the template.",
  },
  {
    heading: "Roadmap",
    body: "Pinia store wiring when shared state shows up. A command palette overlay. Optional drag-out tabs. Native menu integration via the Wails menu API. None of this is committed — additions happen only when a real use case demands them.",
  },
  {
    heading: "Contact",
    body: "Issues and pull requests welcome on the project repository. For private inquiries, use the email listed in the package metadata. Bug reports with a minimal reproduction will always be addressed before feature requests.",
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
              <div class="mark">أ</div>
              <div>
                <div class="name">{{ title }}</div>
                <div class="ver">version {{ version }}</div>
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
  border: 1px solid var(--border);
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
  background: var(--ink);
  color: var(--paper);
  display: grid; place-items: center;
  font-family: var(--arabic);
  font-size: 22px;
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
  padding: 0 12px;
  background: var(--ink);
  color: var(--paper);
  border: none;
  border-radius: 6px;
  font: inherit;
  font-size: var(--fs-small);
  cursor: pointer;
}
.btn:hover { opacity: 0.9; }

.fade-enter-active, .fade-leave-active { transition: opacity 0.12s ease; }
.fade-enter-from, .fade-leave-to { opacity: 0; }
</style>
