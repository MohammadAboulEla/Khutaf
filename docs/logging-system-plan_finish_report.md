# Khutaf — Structured Logging & Critical Function Telemetry Finish Report

**Plan:** `logging-system-plan.md`  
**Status:** Completed and verified

---

## 1. Summary of Changes

### 1.1 Go Telemetry Core & Wails Bindings
- **`internal/session/telemetry.go`** (new):
  - Defined `LogSubsystem` (`HOOK_NATIVE`, `UNITY_FONT`, `IPC_PIPE`, `PIPELINE_CACHE`, `TRANSLATOR`, `ARABIC_SHAPER`).
  - Defined `TelemetryEvent`, `FunctionHealthStats`, `TelemetryBatch`, and `TelemetrySnapshot`.
  - Implemented `telemetryStore` with a thread-safe 2,000-entry ring buffer, pending batch queue, and per-function call/error/timeout/min/max/avg aggregation.
  - Implemented `ParseNativeTelemetry` to parse `[telemetry|<SUBSYSTEM>|<FUNC>|<DURATION_US>|<LEVEL>] <MESSAGE> | key=val ...` lines emitted by `khutaf-hook64.dll` over `Msg::Log` without changing `PROTO_VERSION = 1`.
  - Added `Session.RecordSpan`, `Session.GetTelemetrySnapshot`, `Session.ClearTelemetry`, and `Session.ExportDiagnosticBundle` (writes a `.jsonl` bundle with `Translator.LLM.APIKey` masked as `"***"`, status, stats, ring-buffer events, and `llama-server` recent logs).
- **`internal/session/session.go`**:
  - Added `EventTelemetryBatch = "telemetry:batch"`, `telemetry *telemetryStore`, and `droppedEvents atomic.Uint64`.
  - Added a 100 ms background ticker in `New()` to flush pending telemetry batches to the UI (stopped via `s.eventStop` in `Close()`).
  - Incremented `s.droppedEvents` in `Session.post()` when `s.events` (`cap 4096`) is full.
  - Wired `ParseNativeTelemetry` into `Session.OnHostInfo` and connected telemetry callbacks for `Cache`, `Manager`, `LlamaCppManager`, and `Host`.
  - Instrumented `Session.Attach` (`hook.Inject`), `Session.ShapeForEmbed` (`arabic.Process`), and `Session.handleEmbed` (per-stage microsecond breakdown, glossary mode, placeholder rejection `warn`, and `>80% TimeoutMs` `warn`).
- **`app.go`**:
  - Exposed `GetTelemetrySnapshot()`, `ClearTelemetry()`, and `ExportDiagnosticBundle()` to the Wails frontend.

### 1.2 Go Subsystem Instrumentation
- **`internal/pipeline/cache.go`**: Added `SetTelemetry`; instrumented `Cache.Get` (hit tier: `manual_game`, `manual_global`, `auto_game`, `auto_global`, `shared`, `miss`) and `Cache.PutAuto` (SQLite error / `>50ms` slow write `warn`).
- **`internal/translator/manager.go`**: Added `SetTelemetry`; instrumented `Manager.TranslateValidated` for singleflight coalescing (`waiters`), semaphore wait `>500ms` (`warn`), empty-output rejections (`warn`), and provider errors.
- **`internal/translator/bing.go`**: Added `SetTelemetry`; instrumented `Bing.getSession` duration/errors and `Bing.Translate` token-expiry refresh retries (`warn`).
- **`internal/translator/llamacpp.go`**: Added `SetTelemetry`; instrumented `LlamaCppManager.Start` readiness, Job Object status, health-check timeouts, and unexpected exits.
- **`internal/hook/host.go`**: Added `SetTelemetry`; instrumented `Host.EmbedCallback` (`popPending` miss `warn` on late/timed-out translations) and `procSession.serve` pipe connection timing/errors.

### 1.3 Rust Native Hook Instrumentation (`native/khutaf-hook`)
- **`src/lib.rs`**: Added `pub(crate) fn telemetry(subsys, func, dur_us, lvl, msg)` formatting `[telemetry|...]` over `Msg::Log`.
- **`src/unity/il2cpp.rs` & `src/unity/mono.rs`**: Emitted `HOOK_NATIVE` `error` telemetry when `std::panic::catch_unwind` intercepts a panic in `il2cpp_detour_*` or `mono_detour_*`.
- **`src/embed.rs`**: Emitted `IPC_PIPE` `error` on `embed::process` `wait_ms` timeout and `warn` when wait exceeds 80% of `wait_ms`.
- **`src/unity/font.rs`**: Emitted `UNITY_FONT` `warn` when the expected TMP AssetBundle file is missing.
- **`src/hooks.rs`**: Emitted `HOOK_NATIVE` telemetry in `hooks::shutdown` for `IN_FLIGHT` detour drain duration and `error` if detours remain active past the 3-second deadline.

### 1.4 Frontend UI (`frontend/src`)
- **`frontend/src/composables/useSession.js`**:
  - Subscribed to `telemetry:batch` and added `telemetryEvents`, `functionStats`, `droppedEvents`, `refreshTelemetry()`, `clearTelemetry()`, and `exportDiagnosticBundle()`.
- **`frontend/src/views/DebugView.vue`**:
  - Upgraded to the 3-panel **System Telemetry & Function Monitor**:
    - **Panel A (Top Health Bar & Controls):** Total Calls, Cache Hit Ratio, Avg Embed Latency, Native Panics, Dropped Events, Debug Mode / Real Translator toggles, `Export .jsonl`, and `Clear`.
    - **Panel B (Critical Function Health Matrix):** Collapsible table with health status dots (green/amber/red), subsystem badges, calls, errors/timeouts, last/avg/max durations, and click-to-filter integration with Panel C.
    - **Panel C (Live Telemetry Stream & Pipeline Waterfall):** Subsystem filter pills, level filter (`ALL`, `TRACE+`, `INFO+`, `WARN+`, `ERROR`), search input, Pause/Resume toggle, color-coded duration badges, and inline stacked Pipeline Waterfall bars (`Pre` -> `Trans` -> `Post` -> `Shape` -> `IPC`).

### 1.5 Unit Tests
- **`internal/session/telemetry_test.go`** (new):
  - `TestTelemetryRingBufferWrapAround`
  - `TestParseNativeTelemetry`
  - `TestFunctionHealthStatsAggregation`
  - `TestExportDiagnosticBundleMasksAPIKeys`

---

## 2. Verification Results

All checks ran and passed after the final edit:

- **Go Tests & Vet:** `go test -count=1 ./...; go vet ./...` — **PASS**
  - `khutaf/internal/arabic` (pass)
  - `khutaf/internal/hook` (pass)
  - `khutaf/internal/pipeline` (pass)
  - `khutaf/internal/session` (pass)
  - `khutaf/internal/translator/traditional` (pass)
- **Rust Tests:** `cargo test --manifest-path native/Cargo.toml` — **PASS**
  - `khutaf-proto`: 3 passed, 0 failed
  - `khutaf-hook`: compiled cleanly (0 unit tests)
- **Frontend Build:** `npm --prefix frontend run build` — **PASS**
  - Vite production bundle built (`dist/assets/index.cadef4d9.js`, `dist/assets/index.e6ca9aa6.css`).
