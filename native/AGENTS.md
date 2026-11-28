# Native Hook Layer (`native/`)

`native/` is a Rust workspace compiled into `runtime/khutaf-hook64.dll` (via `scripts/build-native.ps1`) and injected into 64-bit Windows game processes by `internal/hook/inject.go`.

## Crate & Module Map

- `native/Cargo.toml` — Workspace root (`resolver = "2"`, shared `[workspace.package]` version, release LTO).
- `native/khutaf-proto/src/lib.rs` — Wire codec (`PROTO_VERSION = 2`, `MAX_FRAME = 16 MiB`, `Msg`, `Settings`). Mirrors `internal/hook/proto.go`.
- `native/khutaf-hook/src/`:
  - `lib.rs` — `DllMain`, `init_thread`, `run()` session loop, `log()`, `telemetry()`.
  - `ipc.rs` — Shared-memory nonce handshake (`Local\khutaf_ipc_<pid>`) + dual one-way named pipes (`_up` / `_down`) + bounded send queue (`SEND_QUEUE_CAP = 512`).
  - `hooks.rs` — Detour management and `hooks::shutdown()` cleanup on detach.
  - `embed.rs` — Blocking/non-blocking translation round-trip, deduplication, `TEXT_RATE_LIMIT`, circuit breaker, async `ReadyUpdate` queue, `VERBOSE_SCAN_PENDING` flag, and own-output feedback guard.
  - `unity/mod.rs` — Engine detection (`UnityMono`, `UnityIl2cpp`, `Unity`, `Unknown`), Hook IDs `1..=14`, `caller_rel_ctx()` RVA stack walker, `on_frame_tick` / `flush_async_updates` / `rebuild_component_layout`, `scan_active_static_ui`.
  - `unity/mono.rs` — Mono API resolution and text detours.
  - `unity/il2cpp.rs` — IL2CPP API resolution and text detours.
  - `unity/font.rs` — OS dynamic font + `TMP_Font_AssetBundles` fallback/shader-donor injection and narrow-`RectTransform` layout wrapping guard (`ensure_text_layout`).

## Lifecycle & Threading Rules

1. **`DllMain` safety:** `DllMain` only calls `DisableThreadLibraryCalls`, stores `MODULE`, and spawns `init_thread`. Never allocate heavily, acquire complex locks, or wait on IPC under the Windows loader lock.
2. **Reconnect loop (`run()`):** `ipc::connect_wait(pid)` → `embed::reset_for_session()` → `unity::detect()` → send `Msg::Hello` → spawn `ipc::serve()` reader thread (calls `embed::release_all()` on disconnect) → `unity::install(engine)` → join reader → `hooks::shutdown()` → `ipc::close()`.
3. **IPC handshake:** Host creates `Local\khutaf_ipc_<pid>` (64-byte block: magic `KHUT`, version `1`, PID, 32-byte ASCII hex nonce) and pipes `\\.\pipe\khutaf_<pid>_<nonce>_up` (DLL → host) and `_down` (host → DLL). Separate unidirectional handles prevent synchronous-I/O deadlocks between blocking reads and game threads.

## Unity Hooking & Telemetry Invariants

- **Hook IDs `1..=14`** in `unity/mod.rs` are fixed identifiers. Helper predicates `is_tmp_hook`, `is_tmp_like_tags`, and `is_ui_text_hook` govern tag stripping and font target resolution (`resolve_font_target`).
- **Own-output loop prevention:** `embed.rs` keeps `OWN_OUTPUT_HISTORY` (`256`) recent translations so if a game reads `.text` and writes it back, it passes through untouched.
- **StaticUI (`VerboseHooks`):** `send_text_dedup` skips identical consecutive strings (`prev == text`) for `ctx == 0 && ctx2 == 0` hooks without applying the `250 ms` `TEXT_RATE_LIMIT`, preventing simultaneous UI labels in the same frame from being dropped. Toggling `verbose_hooks` from `false` to `true` arms `VERBOSE_SCAN_PENDING` so `on_frame_tick` runs `scan_active_static_ui` on already-open UI.
- **Structured telemetry over `Msg::Log`:** Call `crate::telemetry(subsys, func, dur_us, lvl, msg)` to emit `[telemetry|<subsys>|<func>|<dur_us>|<lvl>] <msg>` without changing `PROTO_VERSION`.

## Verification

```powershell
cargo test --manifest-path native/Cargo.toml
cargo clippy --manifest-path native/Cargo.toml --all-targets -- -D warnings
powershell -ExecutionPolicy Bypass -File scripts/build-native.ps1
```
