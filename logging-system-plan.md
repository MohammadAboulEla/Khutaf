# NEW PLAN : Most Recent
# Khutaf — Structured Logging & Critical Function Telemetry Plan (`logging-system-plan.md`)

## 1. Executive Summary & Objectives

Following the remediation of the P0–P3 findings across the Go backend, Rust native hook (`khutaf-hook64.dll`), and Vue 3 frontend, **Khutaf** requires a unified, zero-overhead telemetry and structured logging architecture. Because Khutaf executes inside a real-time game loop (via MinHook detours on Unity's main thread) and coordinates asynchronous IPC, SQLite caching, regex/glossary pipelines, and external LLM/web translators, silent failures or latency spikes directly freeze or corrupt the game experience.

This plan specifies:
1. **A Unified Structured Telemetry Schema** across Go (`internal/*`) and Rust (`native/khutaf-hook`).
2. **A Critical Function Watchlist & Span Instrumentation** tied directly to the high-risk functions identified during the code review.
3. **Low-Overhead Transport & Ring-Buffer Storage** (preventing UI overwhelm and game-thread stalls, with explicit dropped-event counters).
4. **A Dedicated Monitoring & Function Telemetry Tab** (expanding `DebugView.vue` into a full real-time observability console with subsystem filters, latency alerts, and `.jsonl` diagnostic export).

---

## 2. Architecture & Data Schema

### 2.1 Subsystem Categories (`Subsystem`)
Every log entry and function trace span must belong to one of six explicit subsystems:
- `HOOK_NATIVE`: Rust DLL lifecycle, MinHook detours, `catch_unwind` recoveries, Mono/IL2CPP C-API resolution (`lib.rs`, `il2cpp.rs`, `mono.rs`, `hooks.rs`).
- `UNITY_FONT`: TMP AssetBundle loading, OS dynamic font creation, shader donor extraction, `HasCharacter` checks, font scaling (`font.rs`, `unity_fonts.go`).
- `IPC_PIPE`: Named pipe connection lifecycle, frame encode/decode errors, pending request queues, timeouts (`host.go`, `ipc.rs`, `embed.rs`).
- `PIPELINE_CACHE`: Pre/Post regex rules, Glossary application vs. prompt injection, placeholder multiset validation, SQLite/memory cache hits & writes (`cache.go`, `rules.go`, `glossary.go`).
- `TRANSLATOR`: Provider execution (`Bing`, `LLM`, `OfflineLLM`, `Traditional`), singleflight deduplication, semaphore queue wait time, `llama-server.exe` process supervision (`manager.go`, `bing.go`, `llamacpp.go`, `llm_general.go`).
- `ARABIC_SHAPER`: Logical line wrapping, rich-text tag balancing across lines, contextual shaping, BiDi visual ordering (`wrap.go`, `shaper.go`, `bidi.go`).

### 2.2 Severity Levels (`Level`)
- `TRACE` (0): High-frequency per-frame/per-line events (only emitted when Debug/Telemetry Mode is active).
- `DEBUG` (1): Detailed operational milestones (e.g., cache hit layer, resolved template name, pipe connected).
- `INFO` (2): Lifecycle events (e.g., game attached, hook installed, font bundle loaded, `llama-server` ready).
- `WARN` (3): Recoverable anomalies (e.g., placeholder dropped, `wait_ms` timeout neared, Bing token refresh, fallback to memory cache/store).
- `ERROR` (4): Hard failures or caught panics (e.g., `catch_unwind` triggered in FFI detour, DLL injection failed, SQLite write failed, provider HTTP 4xx/5xx).

### 2.3 Unified Go Telemetry Structs (`internal/session/telemetry.go`)

```go
package session

type LogSubsystem string

const (
	SubHookNative    LogSubsystem = "HOOK_NATIVE"
	SubUnityFont     LogSubsystem = "UNITY_FONT"
	SubIPCPipe       LogSubsystem = "IPC_PIPE"
	SubPipelineCache LogSubsystem = "PIPELINE_CACHE"
	SubTranslator    LogSubsystem = "TRANSLATOR"
	SubArabicShaper  LogSubsystem = "ARABIC_SHAPER"
)

// TelemetryEvent represents a structured log or critical function execution record.
type TelemetryEvent struct {
	Seq        uint64            `json:"seq"`
	Timestamp  string            `json:"timestamp"`  // "15:04:05.000"
	UnixMs     int64             `json:"unixMs"`
	Level      string            `json:"level"`      // "trace" | "debug" | "info" | "warn" | "error"
	Subsystem  LogSubsystem      `json:"subsystem"`
	Func       string            `json:"func"`       // e.g. "Bing.getSession", "embed::process"
	Message    string            `json:"msg"`
	DurationUs int64             `json:"durationUs,omitempty"`
	Fields     map[string]any    `json:"fields,omitempty"`
}

// FunctionHealthStats aggregates real-time metrics per watched critical function.
type FunctionHealthStats struct {
	Func         string       `json:"func"`
	Subsystem    LogSubsystem `json:"subsystem"`
	Calls        uint64       `json:"calls"`
	Errors       uint64       `json:"errors"`
	Timeouts     uint64       `json:"timeouts"`
	LastDurUs    int64        `json:"lastDurUs"`
	MaxDurUs     int64        `json:"maxDurUs"`
	AvgDurUs     int64        `json:"avgDurUs"`
	LastError    string       `json:"lastError,omitempty"`
	LastCalledAt string       `json:"lastCalledAt"`
}
```

---

## 3. Critical Function Watchlist & Instrumentation Points

The AI coding agent must instrument the following critical functions (which correspond directly to our code review fixes) so their call counts, latencies, errors, and state transitions appear in the Monitoring Tab:

### 3.1 Rust Native DLL (`native/khutaf-hook`)
Instead of unstructured text logs, format structured telemetry lines over `Msg::Log` using a prefix convention:
`[telemetry|<SUBSYSTEM>|<FUNC>|<DURATION_US>] <MESSAGE> | key=val ...`
(This avoids breaking `PROTO_VERSION = 1` wire compatibility while giving the Go host full structured parsing).

| Function | File | What to Monitor & Log | Alert Condition |
| :--- | :--- | :--- | :--- |
| `detour_*` (`catch_unwind`) | `unity/il2cpp.rs`, `unity/mono.rs` | Log whenever `catch_unwind` catches a panic inside an FFI detour. Include `hook_id`. | `ERROR` immediately on any caught panic. |
| `Il2CppApi::load` / `MonoApi::load` | `unity/il2cpp.rs`, `unity/mono.rs` | Log resolved DLL name, missing optional exports (e.g., `mono_array_length`), and retry counts before `unity::install` succeeds. | `WARN` if optional exports fall back to raw offsets; `ERROR` if required exports fail. |
| `embed::process` | `embed.rs` | Track total blocking time on the game thread (`wait_ms`), `RECENT_TRANSLATIONS` fast-path hits, `OWN_OUTPUTS` loop-prevention drops, and deadline timeouts. | `WARN` if wait time exceeds `80%` of `wait_ms`; `ERROR` on timeout (`now >= deadline`). |
| `font::reload_fonts` & `apply_component_font` | `unity/font.rs` | Log detected `unity_version`, chosen bundle (`pick_bundle`), shader donor acquisition, `HasCharacter` glyph misses switching from bundle to dynamic OS font, and GC handle frees. | `WARN` if bundle file is missing or `CreateFontAsset` returns null. |
| `hooks::shutdown` | `hooks.rs` | Log `IN_FLIGHT` detour count at detach start and time taken to drain active game threads. | `ERROR` if `IN_FLIGHT > 0` when the 3-second deadline expires. |

### 3.2 Go Host & Pipeline (`internal/*`)

| Function | File | What to Monitor & Log | Alert Condition |
| :--- | :--- | :--- | :--- |
| `hook.Inject` / `injectPID` | `internal/hook/inject.go` | Log target `pid`, `VirtualAllocEx`, `CreateRemoteThread` wait duration, and post-wait `isModuleLoaded` verification. | `ERROR` on `ERROR_ACCESS_DENIED`, timeout, or `isModuleLoaded == false`. |
| `procSession.serve` & `EmbedCallback` | `internal/hook/host.go` | Log pipe connection time (`up`/`down`), FIFO `pending` queue depth, and unmatched `popPending` calls (late translations after timeout). | `WARN` if `popPending` returns `false` (translation arrived too late for the game thread). |
| `Session.handleEmbed` & `translateWith` | `internal/session/session.go` | Record per-stage microsecond breakdown (`PreRulesUs`, `TranslateUs`, `PostRulesUs`, `ShapeUs`, `CallbackUs`, `TotalUs`), active glossary mode (`Apply` vs `PromptContext`), and placeholder check results. | `WARN` when `PlaceholdersPreserved` rejects a translation (include dropped tokens in `Fields`). |
| `Cache.Get` / `PutAuto` / `PutManual` | `internal/pipeline/cache.go` | Track hit tier (`manual_game`, `manual_global`, `auto_game`, `auto_global`, `shared`, `miss`) and SQLite background write latency (`dbMu`). | `WARN` if SQLite write takes `> 50ms` or returns `SQLITE_BUSY`. |
| `Manager.Translate` | `internal/translator/manager.go` | Track singleflight coalesced waiter count (`inflight` hits), semaphore queue wait time (`m.sem`), and empty-output rejections. | `WARN` if semaphore wait time exceeds `500ms`. |
| `Bing.getSession` & `postTranslate` | `internal/translator/bing.go` | Log token/IG/IID extraction duration, HTTP redirect hops, and token-expiry retry triggers. | `WARN` on retry; `ERROR` if regex extraction fails. |
| `LlamaCppManager.Start` & `probeHealth` | `internal/translator/llamacpp.go` | Log process PID, Job Object assignment status, port, startup health-poll duration, and unexpected exit codes. | `ERROR` on health timeout or process crash. |
| `arabic.Process` (`Wrap`, `Shape`, `VisualOrder`) | `internal/arabic/wrap.go`, `bidi.go` | Track input/output rune lengths, line count after wrap, balanced rich-text tag count, and multi-word LTR token count. | `WARN` if unbalanced tags are detected in source text. |

---

## 4. Transport, Ring-Buffer & Backpressure Design

To guarantee that telemetry never degrades game FPS or freezes the Wails WebView:

1. **Backend Ring Buffer & Aggregator (`internal/session/telemetry.go`):**
   - Maintain a thread-safe ring buffer of the last **2,000 `TelemetryEvent`s** in Go memory and a map of **`FunctionHealthStats`** keyed by function name.
   - Maintain an atomic counter `droppedEvents atomic.Uint64` in `Session`. Whenever `s.events` channel (`cap 4096`) is full in `s.post()`, increment `droppedEvents` instead of failing silently.
2. **Batched UI Emission:**
   - Do **not** emit a separate Wails IPC event for every single `TRACE`/`DEBUG` log line if burst rate exceeds 50 events/sec.
   - Use a 100ms ticker in `Session` that flushes pending `TelemetryEvent` slices in a single batch event `telemetry:batch` along with updated `FunctionHealthStats` and `droppedEvents`.
3. **Diagnostic Bundle Export (`ExportDiagnosticLog`):**
   - Add a Wails-bound method `ExportDiagnosticBundle() (string, error)` on `App` / `Session`.
   - When clicked in the UI, it writes a `.jsonl` (or `.zip`) report containing:
     1. Current `Status` and sanitized `Config` (with `APIKey` masked as `"***"`).
     2. Snapshot of all `FunctionHealthStats`.
     3. The full 2,000-entry `TelemetryEvent` ring buffer and recent `llama-server` logs.

---

## 5. Frontend UI Design: Dedicated Monitoring Tab (`DebugView.vue`)

Upgrade `frontend/src/views/DebugView.vue` and `frontend/src/composables/useSession.js` into a comprehensive **System & Function Monitor** tab divided into three collapsible panels:

### Panel A: Top Health Bar & Controls
- **Live Counters:** Total Calls, Cache Hit Ratio (`%`), Active In-Flight Translations (`0..4`), Average Embed Latency (`ms`), Caught Native Panics (`0`), and Dropped UI Events (`0`).
- **Controls:**
  - Toggle: **Enable Deep Telemetry (`DebugMode`)**
  - Toggle: **Use Real Translator in Debug (`DebugRealTrans`)**
  - Button: **Export Diagnostic Bundle (`.jsonl`)**
  - Button: **Clear Telemetry**

### Panel B: Critical Function Watch Table (Real-Time Health Matrix)
A compact, sortable status table displaying `FunctionHealthStats` for every watched function across Go and Rust:
- **Columns:** `Status Dot` (Green = 0 errors & fast, Yellow = warnings/slow, Red = errors/timeouts) | `Subsystem Badge` | `Function Name` | `Calls` | `Errors / Timeouts` | `Last (ms)` | `Avg (ms)` | `Max (ms)` | `Last Error / Note`.
- Clicking any function row filters **Panel C** to show only events and traces emitted by that function.

### Panel C: Unified Live Stream & Pipeline Waterfall Inspector
- **Filter Bar:**
  - Subsystem pills: `ALL`, `HOOK_NATIVE`, `UNITY_FONT`, `IPC_PIPE`, `PIPELINE_CACHE`, `TRANSLATOR`, `ARABIC_SHAPER`.
  - Level filter: `ALL`, `TRACE+`, `INFO+`, `WARN+`, `ERROR ONLY`.
  - Search input: filter by function name, hook key, or source/translated text.
- **Pipeline Waterfall Bar (for `handleEmbed` traces):**
  - Visual horizontal stacked bar showing proportion of time spent in:
    `[PreRules] -> [Translate] -> [PostRules] -> [Arabic Shape/BiDi] -> [IPC Callback]`
  - Color-coded total duration badge:
    - `< 15ms` (Cached / Fast): **Green**
    - `15ms – 800ms` (Normal Network/LLM): **Blue**
    - `> 80% of TimeoutMs`: **Amber (Warning)**
    - `Error / Timeout / Placeholder Dropped`: **Red**

---

## 6. Step-by-Step Implementation Tasks for the AI Agent

1. **Step 1 — Go Telemetry Core (`internal/session/telemetry.go`):**
   - Implement `TelemetryEvent`, `FunctionHealthStats`, the 2,000-entry ring buffer, `RecordSpan(subsystem, fnName, start, err, fields)`, and the `[telemetry|...]` parser in `Session.OnHostInfo`.
   - Add `droppedEvents atomic.Uint64` to `Session.post()` and expose `GetTelemetrySnapshot()` and `ExportDiagnosticBundle()` to Wails in `app.go`.
2. **Step 2 — Instrument Go Critical Functions:**
   - Add `RecordSpan` / structured telemetry calls to `session.go` (`handleEmbed`, `translateWith`), `cache.go` (hit tier & DB write latency), `manager.go` (singleflight & semaphore wait), `bing.go` (`getSession` & retry), `llamacpp.go`, and `host.go` (`popPending` miss warning).
3. **Step 3 — Instrument Rust Native Hook (`native/khutaf-hook`):**
   - Add a lightweight helper `crate::telemetry(subsys, func, dur_us, lvl, msg)` in `lib.rs` that emits formatted `[telemetry|...]` messages when connected.
   - Instrument `catch_unwind` blocks in `il2cpp.rs` and `mono.rs`, timeouts in `embed.rs`, font fallback decisions in `font.rs`, and drain wait in `hooks.rs`.
4. **Step 4 — Frontend Composable & `DebugView.vue` Upgrade:**
   - In `useSession.js`, subscribe to `telemetry:batch` (or update `debug:trace` and `log` handlers), store `functionStats` and `droppedEvents`, and wire `exportDiagnosticBundle()`.
   - Update `DebugView.vue` to render the **Top Health Bar**, **Critical Function Watch Table**, and **Subsystem/Level Filterable Stream**.
5. **Step 5 — Verification & Unit Tests:**
   - Add `internal/session/telemetry_test.go` testing ring-buffer wrap-around, `[telemetry|...]` parsing from Rust, `FunctionHealthStats` min/max/avg calculation, and API key masking during diagnostic export.

# OLD PLAN : Some of its instructuions may be useful
# Khutaf — Telemetry & Function Monitor Plan (`logging-system-plan.md`)

## Overview

Khutaf already has the foundation for observability via `EventLog`, `EventDebugTrace` (`DebugTrace`), and `DebugView.vue`. This plan extends that foundation into a centralized **Telemetry & Function Monitor** tab that tracks critical functions across the Go backend and Rust native DLL (`khutaf-hook`) in real time with minimal overhead on Unity's main thread.

---

## 1. Monitored Subsystems (6 Filterable Categories)

Every telemetry event and function span carries a `category` enum so the UI can toggle each subsystem independently:

| Category | Scope | Key Functions / Events Tracked |
| --- | --- | --- |
| `HOOK_NATIVE` | Rust Detours & Embed Gate | `il2cpp_detour_1/2`, `mono_detour_1/2`, `catch_unwind` panic interceptions, `embed::process`, `is_own_output`, `RECENT_TRANSLATIONS` hits |
| `UNITY_FONT` | Unity Font Injection | `reload_fonts`, `load_bundle_font`, `load_os_fonts`, `create_tmp_font_asset`, `has_all_non_ascii`, `scale_component_font`, `gchandle_pin`/`gchandle_free` |
| `IPC_PIPE` | Named Pipe & Wire Protocol | `createOverlappedPipe`, `ps.serve` connect/disconnect, `readMsg` / `decodeBody` / `Msg::decode` errors, `writeAll` timeouts, `EmbedCallback` latency |
| `PIPELINE_CACHE` | Rules, Glossary & Cache | `PreRules.Apply`, `PostRules.Apply`, `Glossary.Apply` / `PromptContext`, `PlaceholdersPreserved`, `Cache.Get` (hit/miss/scope), `Cache.PutAuto` / `PutManual` |
| `TRANSLATOR` | Providers & Local LLM | `Bing.getSession` / `postTranslate` (redirects, token refresh), `LLM` / `OfflineLLM` (`doChatCompletion`, `<think>` strip), `LlamaCppManager` health/startup, `Manager.Translate` singleflight & semaphore wait |
| `ARABIC_SHAPER` | Text Shaping & BiDi | `arabic.Wrap` + `balanceRichTextTags`, `arabic.Shape`, `arabic.VisualOrder` (LTR run merging) |

---

## 2. Critical Function Spans & Threshold Alerts

### Structured Span Schema (`TelemetryEvent`)
```go
type TelemetryEvent struct {
	Seq        uint64 `json:"seq"`
	Timestamp  string `json:"ts"`         // RFC3339Nano or HH:MM:SS.mmm
	Category   string `json:"category"`   // One of the 6 subsystems above
	Level      string `json:"level"`      // "debug" | "info" | "warn" | "error"
	SpanName   string `json:"span"`       // e.g. "Bing.postTranslate", "arabic.Process"
	DurationUs int64  `json:"durationUs"` // Execution time in microseconds (0 for point events)
	Status     string `json:"status"`     // "ok" | "slow" | "timeout" | "panic" | "dropped" | "error"
	Message    string `json:"msg"`
	Meta       map[string]any `json:"meta,omitempty"` // hookKey, cached, provider, gameUid, etc.
}
```

### Latency & Alert Color Coding (UI)
Thresholds are evaluated relative to the active `Embed.TimeoutMs` (`wait_ms`, default `1000ms`):
- **Green (`ok`):** `DurationUs < 5000` (`< 5 ms`).
- **Yellow / Amber (`slow`):** `DurationUs >= 5000` and approaching `wait_ms` (e.g. `>= 50%` of `Embed.TimeoutMs`, or `>= 5 ms` for hot-path native/shaper functions).
- **Red (`alert`):**
  - Execution exceeded `Embed.TimeoutMs` (`timeout`).
  - `std::panic::catch_unwind` caught a panic inside an IL2CPP/Mono detour (`panic`).
  - `PlaceholdersPreserved` rejected a translation (`dropped`).
  - Named pipe frame decode error, `writeAll` failure, or translator HTTP/process error (`error`).

### Alert Summary Counters
Header badges in the Telemetry tab track cumulative counts for the active session:
- `panicsCaught` (Rust FFI `catch_unwind`)
- `timeouts` (Embed / Translator / IPC)
- `placeholdersDropped`
- `droppedEventsCount` (Ring buffer overflow counter)

---

## 3. Zero-Overhead Ring Buffer, Batching & Export

### Rust Native Side (`khutaf-hook`)
- **Non-blocking emission:** Detours on Unity's main thread never perform disk I/O or wait on UI locks.
- **Bounded native log/span queue:** Use a fixed-capacity ring buffer (e.g. `1024` slots) drained by the existing IPC sender thread, incrementing an atomic `DROPPED_TELEMETRY: AtomicU64` counter if full (avoiding allocation storms under heavy Unity `set_text` loops).
- **Optional `khutaf-proto` extension:** Extend `Msg::Log` (or add `Msg::Telemetry`) to carry `category`, `duration_us`, and `status` when verbose native tracing is enabled.

### Go Backend Side (`internal/telemetry` + `Session`)
- **Ring Buffer:** Replace unbounded per-event `s.emit` calls with a fixed-capacity ring buffer (e.g. `4096` `TelemetryEvent` entries) protected by a lightweight mutex.
  - When full, overwrite the oldest entry (or drop incoming) and atomically increment `droppedEventsCount`.
- **100ms Batch Flusher:** A single background ticker (`100ms`) drains pending events from the ring buffer and emits one Wails event (`EventTelemetryBatch`) containing:
  ```go
  type TelemetryBatch struct {
      Events             []TelemetryEvent `json:"events"`
      DroppedEventsCount uint64           `json:"droppedEventsCount"`
  }
  ```
- **Session History Retention:** Keep the last `N` (e.g. `2000`) events in memory for filtering and export without re-querying.

### Frontend (`TelemetryView.vue` / Evolved `DebugView.vue`)
- **6 Category Filter Pills** + **Level Filter** (`All`, `Warn+`, `Error/Alert`) + **Text Search**.
- **Pause / Resume Live Stream** & **Clear** controls.
- **Export Support (Wails Save File Dialog):**
  - `.jsonl`: One JSON-serialized `TelemetryEvent` per line (plus header line with app version, OS, active provider, and `droppedEventsCount`) for automated analysis.
  - `.log`: Human-readable `[timestamp] [CATEGORY] [LEVEL] span (1234µs): message` format for sharing in bug reports.

---

## 4. Phased Implementation Order

1. **Phase 1 — Go Telemetry Hub (`internal/telemetry`):** Implement ring buffer, `droppedEventsCount`, 100ms batch emitter, and `.jsonl`/`.log` export methods on `Session` / `App`.
2. **Phase 2 — Instrument Critical Paths:** Wrap `Session.handleEmbed`, `Translator.Manager`, `Bing`, `LLM`/`OfflineLLM`, `LlamaCppManager`, `Cache`, and `arabic.Process` with span recording.
3. **Phase 3 — Native Hook Telemetry:** Add bounded ring buffer + `catch_unwind` panic / `font.rs` span reporting in `khutaf-hook` over IPC.
4. **Phase 4 — Vue 3 Telemetry Tab:** Upgrade `DebugView.vue` (or add `TelemetryView.vue`) with category filters, latency color badges, alert counters, and `.jsonl`/`.log` export.
