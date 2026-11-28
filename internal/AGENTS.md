# Go Backend & Host Layer (`app.go`, `internal/`)

The Go layer hosts the Wails v2 desktop app, manages Windows IPC and DLL injection, executes the text/translation/Arabic pipeline, and persists portable configuration.

## Package Map

| Package / Files | Build Tag | Responsibility |
| :--- | :--- | :--- |
| Root (`main.go`, `app.go`, `app_window.go`, `app_fonts_windows.go`, `app_hotkeys.go`, `app_updater.go`, `tray_windows.go`) | `windows` (except `main.go`) | Wails `App` facade, lifecycle (`startup`, `beforeClose`, `shutdown`), window & standalone hooks popup geometry, system tray, update polling loop, global hotkeys, Win32 GDI font enumeration (`listSystemFonts`). |
| `internal/session` (`session.go`, `lifecycle.go`, `embed.go`, `smart_embed.go`, `hook_intel.go`, `profile_io.go`, `games.go`, `cache.go`, `callbacks.go`, `auto_attach.go`, `benchmark.go`, `telemetry.go`, `unity_fonts.go`) | `windows` | Core orchestrator (`Session`): config/lifecycle, attach/detach, `hook.Handler` callbacks, `embedCh` pipeline worker & rolling LLM story context, smart hook classification/history/presets/mute, sanitized `.khutaf.json` profile I/O & quick glossary capture, saved games & cache CRUD, provider benchmarking, telemetry ring buffer (`2000` events + `FunctionHealthStats`), diagnostic `.jsonl` export, Unity font bundle downloader. |
| `internal/hook` (`host.go`, `proto.go`, `process.go`, `inject.go`, `types.go`) | Mixed (`proto.go`, `types.go` cross-platform) | Overlapped named-pipe IPC server (`Host`), `Local\khutaf_ipc_<pid>` handshake mapping with owner-only security descriptor, wire codec (`proto.go`), process enumeration, `CreateRemoteThread(LoadLibraryW)` injection. |
| `internal/translator` (`manager.go`, `translator.go`, `bing.go`, `llm_general.go`, `llm_offline.go`, `llamacpp.go`, `traditional/*`) | Cross-platform | `Provider` interface, `Manager` (single-flight `inflight` dedup, semaphore `maxConcurrentRequests = 4`, primary + fallback routing, cache integration), OpenAI-compatible & offline GGUF prompt templates, `llama-server.exe` process runner, 12 free traditional web providers in `traditional/`. |
| `internal/pipeline` (`cache.go`, `cache_sqlite_windows.go`, `cache_sqlite_other.go`, `rules.go`, `glossary.go`) | Mixed | SQLite/in-memory translation cache scoped by normalized game UID + provider, pre/post regex & literal rules, glossary term replacement & LLM prompt injection, placeholder preservation check (`[..]` / `{..}`). |
| `internal/arabic` (`shaper.go`, `bidi.go`, `wrap.go`) | Cross-platform | Contextual Arabic glyph shaping, visual BiDi reordering for Unity/LTR renderers, line wrapping (`arabic.Process`). |
| `internal/config` (`config.go`, `dpapi_windows.go`, `dpapi_other.go`) | Mixed | Portable `userconfig/` store (`config.json`, `games.json`, `glossary.json`), per-game overrides (`EmbedOverride`, `TranslatorOverride`, `Pipeline`), DPAPI encryption for `LLMSettings.APIKey`. |
| `internal/hotkeys` (`hotkeys_windows.go`) | `windows` | Global Windows hotkey registration and dispatch. |
| `internal/updater` (`updater.go`, `semver.go`, `apply_windows.go`) | Mixed | GitHub Releases API check with ETag caching, SHA-256 verification, in-place Windows replacement. |

## Concurrency & Architectural Invariants

1. **Lock Discipline (`Session.mu`):** Never hold `Session.mu` across blocking operations (HTTP translation calls, named-pipe writes, or disk persistence). Read-hot structures use `atomic.Pointer` (`rules`, `embedCfg`) or `atomic.Bool`/`Uint64` (`paused`, `debugMode`, `debugRealTrans`, `debugSeq`).
2. **Never Block Game Threads:** `Session.OnEmbedText` enqueues onto `embedCh` and immediately calls `s.host.EmbedCallback(tp, text, "")` if the channel is full. `handleEmbed` must always invoke `s.host.EmbedCallback` once per request to release the waiting game thread (unless `NonBlocking` already released it on the DLL side).
3. **Wire Protocol Sync:** `internal/hook/proto.go` mirrors `native/khutaf-proto/src/lib.rs`. Keep `protoVersion = 2` and golden tests in `internal/hook/proto_test.go` synchronized.
4. **Linux CI Cross-Check:** Packages `./internal/arabic`, `./internal/config`, `./internal/hook`, `./internal/pipeline`, and `./internal/translator/...` are tested on `ubuntu-latest` in CI. Isolate any `golang.org/x/sys/windows` code behind `*_windows.go` with `//go:build windows` and provide `*_other.go` stubs where needed.
5. **Telemetry Subsystems:** Use the canonical `LogSubsystem` constants in `internal/session/telemetry.go`: `HOOK_NATIVE`, `UNITY_FONT`, `IPC_PIPE`, `PIPELINE_CACHE`, `TRANSLATOR`, `ARABIC_SHAPER`.

## Verification

```powershell
go vet ./...
go test -race -count=1 ./...
```
