# Frontend Layer (`frontend/`)

The UI is strictly a Windows desktop Wails v2 (WebView2) interface built with Vue 3 (`<script setup>`) and bundled by Vite 3. **Khutaf will never be a web app** — do not introduce web-browser fallback backends, REST/WebSocket shims, or SSR logic.

## Directory Layout (`frontend/src/`)

- `App.vue` — Top-level shell, tab routing, modal orchestration (`AboutModal`, `UpdateModal`, `FontDownloadModal`, `OnboardingWizard`, `TroubleshooterModal`, `QuickHooksModal`), and window close/tray handling.
- `views/` — 8 main tabs: `QuickPlayView.vue`, `HookConsoleView.vue`, `GamesView.vue`, `EmbedSettingsView.vue`, `HotkeysView.vue`, `TranslatorsView.vue`, `TextPipelineView.vue`, `DebugView.vue`.
- `components/` — Reusable modals/panels plus `components/hook/` (`HookBrowser.vue`, `HookTable.vue`, `ArabicInspector.vue`, `ProcessBar.vue`).
- `shell/` — Layout chrome (`ShellLayout.vue`, `ShellHeader.vue`, `ShellSidebar.vue`, `ShellMain.vue`, `ShellFooter.vue`).
- `composables/`:
  - `useSession.js` — Coordinator composable composing domain modules and initializing Wails event listeners (`init()`).
  - `wailsBridge.js` & `sessionState.js` — Safe guarded wrapper over generated `wailsjs/go/main/App.js` bindings, `window.runtime.EventsOn`, and shared `errorBanner`/`saveState`.
  - Domain composables: `useHooks.js`, `useConfig.js`, `useUpdater.js`, `useUnityFonts.js`, `useBenchmark.js`, `useTelemetry.js`.
  - UI preference composables: `useI18n.js`, `useUiMode.js`, `useTheme.js`, `useSidebar.js`, `usePinnedHookSort.js`.
- `i18n/` — Bilingual dictionaries `ar.js` (RTL) and `en.js` (LTR), re-exported via `messages.js` and verified by `messages.test.js`.
- `utils/`:
  - `providers.js`, `hookLabels.js`, `friendlyErrors.js`, and unit tests (`utils.test.js`).
- `frontend/wailsjs/` — Auto-generated Wails TypeScript/JS bindings (`go/main/App.js`, `go/models.ts`, `runtime/`). **Never hand-edit.**

## Critical Contracts & Gotchas

1. **Typed Wails Bridge (`wailsBridge.js`):** `wailsBridge.js` imports typed bindings from `wailsjs/go/main/App.js` and guards against missing `window.go?.main?.App` solely so `node --test` and early Wails WebView startup never crash (not for web-app deployment). Regenerate `wailsjs/` via `wails build` / `wails dev` whenever `app*.go` signatures change.
2. **15 Wails Events Subscribed Across `useSession.js` & Domain Composables:**
   `session:status`, `config:updated`, `hooks:popup`, `hotkey:toast`, `unityfonts:progress`, `updater:status`, `translator:bench:item`, `translator:bench:done`, `hook:new`, `hook:remove`, `hook:output`, `embed:line`, `log`, `debug:trace`, `telemetry:batch`.
   High-frequency streams (`hook:output`, `embed:line`, `log`, `debug:trace`) are batched on a `32 ms` timer (`scheduleFlush`).
3. **Bilingual Parity (`i18n/ar.js`, `i18n/en.js`):** Every new UI key must be added to both `ar.js` and `en.js` with matching `{placeholders}` (enforced by `messages.test.js`). Check `simple` vs `advanced` visibility (`useUiMode.js`) when adding controls.
4. **Provider Registry Sync:** `src/utils/providers.js` (`TRADITIONAL_PROVIDERS`, `ALL_PROVIDERS`) must match Go's `translator.BuildProvider` (`internal/translator/manager.go`) and `traditional.New` (`internal/translator/traditional/common.go`).
5. **Custom Tooltips Only (`Tooltip.vue`):** Every tooltip in the UI MUST use the reusable `<Tooltip>` component (`frontend/src/components/Tooltip.vue`). Never use native HTML `title` or `:title` attributes on elements.

## Verification

```powershell
npm --prefix frontend test
npm --prefix frontend run build
```

Note: Built-in `node --test src` runs i18n parity and `utils/` tests; ESLint and TypeScript checking are not configured yet.
