# Khutaf Refactoring & Scalability Roadmap

**Strategic Direction:** Keep Khutaf focused on Unity Mono/IL2CPP excellence in the near term while organizing large multi-responsibility files into focused modules and introducing clean boundaries so future game engines can be added without rewriting core pipelines.

This roadmap is ordered by ROI and risk—each phase can be executed independently without breaking existing features or wire compatibility.

---

## Current Structural Hotspots (Measured LOC)

| Area | Large Files | Core Friction for AI & Scaling |
| :--- | :--- | :--- |
| **Frontend State & Views** | `useSession.js` (768), `TranslatorsView.vue` (1078), `QuickHooksModal.vue` (1026), `messages.js` (830), `QuickPlayView.vue` (807), `EmbedSettingsView.vue` (806), `TextPipelineView.vue` (773), `DebugView.vue` (761) | Single global composable owns all RPC/events/state via stringly-typed `window.go?.main?.App?.[name]`; no TypeScript, ESLint, or frontend unit tests. |
| **Go Session & Wails Facade** | `internal/session/session.go` (1483), `app.go` (1063), `internal/hook/host.go` (941), `internal/updater/updater.go` (923) | `Session` and `App` mix lifecycle, embed pipeline, smart-embed heuristics, Win32 window geometry, GDI font scanning, and cache CRUD in single files. |
| **Rust Native Hook** | `unity/font.rs` (1645), `embed.rs` (1439), `unity/mono.rs` (961), `unity/mod.rs` (958), `unity/il2cpp.rs` (872) | Font loading/layout, embed state/dedup/async queue/circuit breaker, and direct `unity::detect/install` coupling in `lib.rs`. |
| **Cross-Layer Contracts** | `khutaf-proto/src/lib.rs` ↔ `internal/hook/proto.go`; event names & provider lists | Manual synchronization across Rust, Go, and JS. |

---

## Phase 1 — High-ROI De-Monolithing & Contract Safety (Go + Frontend) *(Completed in v0.2.4)*

- Split `internal/session/session.go` into `session.go`, `lifecycle.go`, `embed.go`, `smart_embed.go`, `games.go`, `cache.go`, and `callbacks.go`.
- Split `app.go` into `app.go`, `app_window.go`, `app_fonts_windows.go`, `app_hotkeys.go`, and `app_updater.go`.
- Split `frontend/src/i18n/messages.js` into `ar.js`, `en.js`, and `messages.js`, backed by `node --test` i18n parity & utility tests (`npm --prefix frontend test`) in CI.
- Split `frontend/src/composables/useSession.js` into `wailsBridge.js`, `sessionState.js`, `useUpdater.js`, `useUnityFonts.js`, `useBenchmark.js`, `useTelemetry.js`, `useHooks.js`, and `useConfig.js`.

---

## Phase 2 — Frontend Component Modularity & Automated Checks *(Partially Completed in v0.2.5)*

**Goal:** Prevent regressions in the Vue layer and break down 800–1000+ line views. **Risk:** Low.

1. **Completed in v0.2.5:**
   - Consolidated the three duplicated hook lists (`QuickPlayView.vue`, `HookTable.vue`, `QuickHooksModal.vue`) into reusable `components/hook/HookBrowser.vue` + `composables/useHookBrowser.js` (`simple` / `picker` / `advanced` variants), shrinking `QuickHooksModal.vue` from 1,026 LOC to ~455 LOC and `HookTable.vue` to a thin compatibility wrapper.
   - Standardized all UI tooltips onto `components/Tooltip.vue` (zero native HTML `title` attributes).
2. **Remaining subcomponent extractions & checks:**
   - `TranslatorsView.vue` → `TraditionalTranslatorPanel.vue`, `OpenAICompatPanel.vue`, `OfflineLlamaPanel.vue`, `ProviderSelector.vue`.
   - `EmbedSettingsView.vue`, `TextPipelineView.vue`, `DebugView.vue` → extract logical sections into `components/<domain>/`.
   - Add ESLint (Vue 3) and optional `tsc --noEmit --allowJs --checkJs` (JSDoc typechecking against `frontend/wailsjs/go/models.ts`) to `.github/workflows/ci.yml`.

**Verification:** `npm --prefix frontend test` + `npm --prefix frontend run lint` + `npm --prefix frontend run build`.

---

## Phase 3 — Native Rust Modularity & Clean Engine Boundary

**Goal:** Tame the largest Rust modules and isolate Unity behind a clean engine seam (ready for future engines when needed). **Risk:** Medium.

1. **Split `native/khutaf-hook/src/unity/font.rs` (1,645 LOC):**
   - `unity/font/mod.rs` (public `on_text` orchestration & cache state)
   - `unity/font/bundle.rs` (AssetBundle font & shader-donor loading)
   - `unity/font/dynamic.rs` (OS dynamic font creation & fallback)
   - `unity/font/layout.rs` (`ensure_text_layout`, narrow-`RectTransform` wrap guard, RTL/alignment helpers)
2. **Split `native/khutaf-hook/src/embed.rs` (1,439 LOC):**
   - `embed/mod.rs` (`process_unity`, public entrypoints, unit tests)
   - `embed/state.rs` (settings, `OWN_OUTPUT_HISTORY`, `RecentCache`, circuit breaker)
   - `embed/async_queue.rs` (`AsyncState`, `ReadyUpdate`, TTL & GC handle cleanup)
   - `embed/tags.rs` (rich-text/TMP tag stripping & restoration)
3. **Extract Win32 transport out of `internal/hook/host.go` (941 LOC):**
   - Move `overlappedPipe` into `internal/hook/pipe_windows.go` and shared-memory handshake / security descriptor helpers into `internal/hook/handshake_windows.go`, keeping `host.go` focused on session orchestration and dispatch.
4. **Introduce a minimal `EngineBackend` seam in `native/khutaf-hook`:**
   - Extract a lightweight `EngineBackend` trait (`detect`, `install`, `on_settings_updated`, `shutdown`) in `native/khutaf-hook/src/engine.rs`, implemented first by `unity::UnityEngine`.
   - Keep `lib.rs` engine-agnostic (`engine::detect_and_install()`) without adding speculative non-Unity engine implementations yet—ensuring future engines (Unreal, Godot, RPG Maker, etc.) plug in cleanly.

**Verification:** `cargo test --manifest-path native/Cargo.toml`, `cargo clippy --manifest-path native/Cargo.toml --all-targets -- -D warnings`, `scripts/build-native.ps1`, and `go test -race -count=1 ./...`.

---

## Phase 4 — Single-Source Cross-Layer Contracts (Optional / Long-Term)

**Goal:** Eliminate manual drift between Rust, Go, and JS. **Risk:** Medium.

1. **Shared contract manifest (or schema-driven generator):**
   - Define message IDs/layouts, Wails event names, and translator provider IDs in a single spec (e.g., JSON/TOML + build-time generator or cross-language parity tests) so `khutaf-proto`, `internal/hook/proto.go`, `internal/translator`, and `frontend/src/utils/providers.js` cannot silently diverge.
2. **Split `internal/updater/updater.go` (923 LOC):**
   - Separate GitHub API client/release resolution, download/SHA-256 verification, and status persistence if updater complexity grows further.
