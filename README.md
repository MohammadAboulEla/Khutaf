<p align="center">
  <img src="build/appicon.png" alt="Khutaf Logo" width="220" />
</p>

<p align="center" style="font-size: 2em; font-weight: bold; margin: 15px 0 0 0;">Khutaf — خُطّاف</p>

<p align="center">
  A fast, lightweight Windows tool focused on making <strong>live Arabic translation</strong> effortless for PC games—hooking and embedding translations directly inside the game UI in real time without modifying game files. Currently supports <strong>64-bit Unity (Mono &amp; IL2CPP)</strong> games, with additional game engines planned on the roadmap. Powered by <strong>Go</strong>, <strong>Wails v2</strong>, <strong>Vue 3</strong>, and <strong>Rust</strong>.
</p>

<p align="center">
  <a href="README.ar.md">العربية</a> | <strong>English</strong>
</p>

[![Watch the video](https://github.com/user-attachments/assets/278497ac-f4b8-4a1c-a0cc-e942f136d3a1)](https://github.com/user-attachments/assets/278497ac-f4b8-4a1c-a0cc-e942f136d3a1)

---

## Download (`v0.1.4-beta`)

Download the latest portable Windows release (`Khutaf.exe` + `runtime/khutaf-hook64.dll`) from [GitHub Releases](https://github.com/MohammadAboulEla/Khutaf/releases):

* Extract the archive anywhere while keeping `Khutaf.exe` and `runtime/khutaf-hook64.dll` together.
* Requires **Windows 10 / 11 (x64)** and **Microsoft Edge WebView2 Runtime**.
* Optionally download `TMP_Font_AssetBundles` from inside **Embed Settings** for Unity games lacking Arabic font glyphs.

---

## Key Features

### 🎯 In-Game Text Hooking & Embedding
* **Self-Authored Rust Hook DLL:** Enumerates running supported games (currently 64-bit Unity Mono & IL2CPP), injects `khutaf-hook64.dll` via `CreateRemoteThread(LoadLibraryW)`, and communicates over owner-only named pipes with per-session random pipe handshakes (`PROTO_VERSION = 2`).
* **Non-Blocking (Async) Translation (Default):** Translates off the game's main thread and writes Arabic back on frame ticks (currently via Unity's `Canvas::SendWillRenderCanvases` / `Camera::FireOnPreCull`) so gameplay never freezes. Configurable pending placeholder (`Original` by default, `"..."`, or custom text) with automatic restoration on timeout or error.
* **Low-Overhead Hook Path & Resilience:** Zero-allocation UTF-16 letter pre-filtering, bounded stack-trace reprobes for disabled UI components, timeout cooldown, circuit breaker with late-reply cache warming, and exponential IPC reconnect backoff.
* **Live Hook Console & Profiles:** Inspects discovered text hooks split by call-site `#ctx` with real-time captured text, per-hook **Embed** toggles, and automatic per-game profile persistence (`games.json`).
* **Flexible Display & Typography:** Show translation only, original + translation, translation + original, or original only; relative font scaling; and optional stripping of invisible typewriter alpha tags (`<color=#00000000>`) while preserving visible colors.

### ✍️ Arabic RTL Engine & Font Injection
* **Contextual Shaping:** Converts Arabic letters to Presentation Forms (`U+FE80`–`U+FEFC` & Forms-A), applies mandatory Lam-Alef ligatures, and preserves Tashkeel diacritics.
* **Smart BiDi Visual Ordering:** Reverses RTL lines for engines without native Arabic support while keeping Latin words, numbers, `[vars]`, `{vars}`, CamelCase placeholders, and paired rich-text tags (`<color=...>...</color>`) intact and mirroring brackets automatically.
* **Logical Line Wrapping:** Wraps text on word boundaries before visual reversal (optionally enforced on non-Arabic output) so multi-line dialogue boxes read top-to-bottom.
* **Font Fallback (Unity Mono & IL2CPP):** Dynamically creates OS TMP/UI fonts (`Tahoma` by default) and provides one-click in-app download of SHA-256-verified `TMP_Font_AssetBundles` (`arialuni_sdf_u*`) so Arabic renders cleanly instead of missing-glyph boxes.

### 🌐 Translation Providers, Pipeline, Cache & Diagnostics
* **Free Traditional Translators:** Bing Translator (default, no API key required), plus Google, Microsoft, Yandex, Papago, Youdao, Alibaba, Volcengine, LingoCloud, ModernMT, QQ TranSmart, and Translate.com.
* **OpenAI-Compatible & Offline Local LLMs:** Presets for OpenAI, Gemini, DeepSeek, Groq, OpenRouter, Ollama, LM Studio, plus a built-in `llama.cpp` (`llama-server.exe`) runner supporting offline translation templates (`HY-MT2`, `TranslateGemma`, `NLLB-Instruct`).
* **Text Pipeline & 3-Tier SQLite Cache:** Pre/post literal & regex rules, global + per-game glossary, placeholder safety checks, and 3-tier SQLite translation cache (per-game -> global -> cross-game shared fallback) with a built-in manual cache editor.
* **Diagnostics & Telemetry View:** Live per-subsystem telemetry stream, function latency matrix, and redacted JSONL diagnostic bundle export.

---

## Build from Source

### Prerequisites
* **Windows 10 / 11** (x64)
* **Go** (1.23 or later)
* **Rust** (Cargo + `msvc` toolchain) to build `runtime/khutaf-hook64.dll`
* **Node.js** & **npm**
* **Wails CLI v2**

### Build Native Hook DLL
```powershell
powershell -ExecutionPolicy Bypass -File scripts/build-native.ps1
```

### Development Run
```bash
wails dev
```

### Production Build
```bash
wails build
```
> Keep the `runtime/` directory (containing `khutaf-hook64.dll`) next to the compiled `Khutaf.exe` executable.

---

## Safety, Anti-Cheat & Privacy
* **Offline Single-Player Games Only:** Khutaf injects a native hook DLL (`khutaf-hook64.dll`) via `CreateRemoteThread(LoadLibraryW)` and includes built-in anti-cheat process detection warnings (e.g., EasyAntiCheat, BattlEye, Riot Vanguard). Do **not** attach to online multiplayer games or anti-cheat-protected titles, as DLL injection may trigger bans or crashes.
* **Local Security & Integrity:** API keys are encrypted at rest via Windows DPAPI, IPC named pipes and shared memory enforce owner-only ACLs, and downloaded font bundles are verified against pinned SHA-256 digests.
* **Translation Privacy:** Captured game dialogue is sent only to whichever translation provider you configure (Bing or your OpenAI-compatible endpoint). Use the built-in `llama.cpp` / Offline LLM mode for 100% local, offline translation.

---

## Technical Architecture

```mermaid
graph TD
    A[Vue 3 Frontend] <-->|Wails IPC| B[App & Session]
    B <-->|Named Pipes| C[khutaf-hook64.dll Rust]
    B --> D[Pipeline, Glossary & Cache]
    B --> E[Translator Bing / LLM]
    B --> F[Arabic Shaper & BiDi]
    B --> G[JSON Config & SQLite Cache]
```

---

## License

Copyright (c) 2026 Mohammad AboulEla.  
Licensed under the [GNU General Public License v3.0 (GPL-3.0)](LICENSE).

---

_Wait for more..._

Support me on PayPal

[![Donate](https://img.shields.io/badge/Support-PayPal-blue.svg)](https://paypal.me/mohammadmoustafa1)
