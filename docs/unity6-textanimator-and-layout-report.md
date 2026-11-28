# Unity 6 / TextAnimator / Dynamic Bubble Investigation & Fix Report

**Date:** 2026-11-27  
**Case Study:** `Tape to Tape` (Unity `6000.0.79f1`, IL2CPP `GameAssembly.dll`, metadata v31)

---

## 1. What We Learned

### 1.1 Why Text Hooks Were Hard to Isolate
- Text in `Tape to Tape` does not go straight from game dialogue/UI scripts to `TMP_Text.set_text`. Instead, it passes through shared middleware layers:
  - `Febucci.UI.TextAnimator` / `TextAnimatorPlayer` (animated dialogue & typewriter control)
  - `Tape2Tape.Speech.SpeechBubbleController` / `SpeechBubbleView` (world/canvas speech bubbles)
  - `I2.Loc.Localize` and `PixelCrushers.LocalizeUI` (UI localization)
- Because `caller_rel_ctx` in `native/khutaf-hook/src/unity/mod.rs` identifies hooks by the immediate native return address, calls funneled through shared middleware (`TextAnimator` or localization components) collapse under the same context or bypass direct `TMP_Text` setter patterns (e.g., `TMP_Text.SetCharArray`), making distinct channels harder to separate.

### 1.2 Why `VERBOSE HOOKS` (`StaticUI`) Failed to Capture Static UI
Three combined factors blocked `StaticUI` capture:
1. **`OnEnable`-only capture:** `il2cpp.rs` and `mono.rs` only inspected `m_text` / `m_Text` inside `OnEnable` detours. Any menu already open when `VERBOSE HOOKS` was toggled on—or any pooled UI object—never fired `OnEnable` again.
2. **Populated after `OnEnable`:** Localization/bubble components (`I2.Loc.Localize.OnLocalize`, `LocalizeUI`, `SpeechBubbleView.Bind`) populate `m_text` *after* `OnEnable`, so the field was often empty or held an unresolved key during `OnEnable`.
3. **Frame-level rate-limit drop:** `handle_static_ui_on_enable` sent all captured text with `ctx = 0, ctx2 = 0`. In `embed.rs`, `send_text_dedup` enforced `TEXT_RATE_LIMIT` (`250 ms`) per `(hook_id, ctx, ctx2)` key, silently dropping all distinct static labels that enabled within the same 250 ms window.

### 1.3 Why Translations Rendered 1–2 Characters per Line
- Arabic contextual shaping and visual BiDi reordering (`internal/arabic`) were working correctly.
- In `Tape2Tape.Speech.SpeechBubbleView`, the layout lifecycle runs:
  `PrepareLayout` → `MeasureBubbleSize` → `ApplyBubbleSize` → `ApplyTextRect` → `PrepareTextHidden` → `TypeAsync`.
- `MeasureBubbleSize` locked a narrow `RectTransform` width before the async Arabic translation was written back (e.g., around a short pending/empty string).
- When `flush_async_updates` or `font::on_text` later wrote the full Arabic text, the bubble width was not recalculated. `TextMeshPro` word-wrapped inside a 1–2 glyph wide rect, while `ensure_text_layout` in `font.rs` switched `overflowMode` to `Overflow` (`0`), causing single/double characters to stack vertically down the screen.

---

## 2. What We Changed (Backward-Compatible)

All existing hook IDs (`1..=13`), `caller_rel_ctx` values, and behavior for previously working games were preserved.

### 2.1 Narrow-Bubble Wrapping Guard & Layout Rebuild
- **[`native/khutaf-hook/src/unity/font.rs`](../native/khutaf-hook/src/unity/font.rs):**
  - Extended `LayoutMethods` with cached pointers for `get_rectTransform`, `RectTransform.get_rect`, `get_fontSize`, `get_textWrappingMode` / `set_textWrappingMode` (Unity 6 TMP), and `get_enableWordWrapping` / `set_enableWordWrapping` (older TMP).
  - Added `read_f32_prop`, `read_u8_prop`, and `read_rect_width`.
  - In `ensure_text_layout`, when `visible_chars >= 6` and `0.5 < rect_width < font_size * 2.5`, TMP word wrapping is switched to `NoWrap` (`0` / `false`) so narrow pre-measured rects do not break text into 1–2 characters per line.
- **[`native/khutaf-hook/src/unity/mod.rs`](../native/khutaf-hook/src/unity/mod.rs):**
  - Extended `AsyncUnityCache` and added `rebuild_component_layout` (invoking `Graphic.SetLayoutDirty()` and `LayoutRebuilder.ForceRebuildLayoutImmediate(rectTransform)` when the component derives from `UnityEngine.UI.Graphic`) right after async text writeback in `flush_async_updates`.

### 2.2 `VERBOSE HOOKS` Active Scene Scan & Dedup Fix
- **[`native/khutaf-hook/src/embed.rs`](../native/khutaf-hook/src/embed.rs):**
  - Added `VERBOSE_SCAN_PENDING`, `has_verbose_scan_pending()`, and `take_verbose_scan_pending()`, set when `verbose_hooks` transitions from `false` to `true` in `set_settings`.
  - Updated `send_text_dedup` so static UI hooks (`ctx == 0 && ctx2 == 0`) still skip identical consecutive text (`prev == text`) but are not dropped by `TEXT_RATE_LIMIT` when multiple distinct labels enable in the same frame.
  - Updated unit test `verbose_hooks_flag_tracks_settings` to verify `VERBOSE_SCAN_PENDING` transitions and reset behavior.
- **[`native/khutaf-hook/src/unity/mod.rs`](../native/khutaf-hook/src/unity/mod.rs), [`il2cpp.rs`](../native/khutaf-hook/src/unity/il2cpp.rs), [`mono.rs`](../native/khutaf-hook/src/unity/mono.rs):**
  - Added `scan_active_static_ui` called from `on_frame_tick` when `take_verbose_scan_pending()` is true, scanning up to 256 active `TMPro.TMP_Text` and `UnityEngine.UI.Text` components via `Object.FindObjectsOfType(Type)` / `Object.FindObjectsByType(Type, FindObjectsSortMode)` and `Behaviour.get_isActiveAndEnabled()`.
  - Updated `detour_canvas_will_render` and `detour_camera_pre_cull` fast-path checks to wake `on_frame_tick` when `embed::has_verbose_scan_pending()` is true.

### 2.3 Optional `Febucci.UI.TextAnimator` Hook
- **[`native/khutaf-hook/src/unity/mod.rs`](../native/khutaf-hook/src/unity/mod.rs), [`il2cpp.rs`](../native/khutaf-hook/src/unity/il2cpp.rs), [`mono.rs`](../native/khutaf-hook/src/unity/mono.rs):**
  - Added `HOOK_FEBUCCI_TEXTANIMATOR_SET_TEXT` (`14`), `is_tmp_like_tags`, and `resolve_font_target` (resolving `get_tmproText()` so font injection and layout fixes still apply to the underlying `TMP_Text`).
  - Conditionally hooked `Febucci.UI.TextAnimator` (`SetText(String, Boolean)`, `SetText(String)`, `set_text(String)`) as `UNITY:TextAnimator.SetText` only when `Febucci.TextAnimator.Runtime` / `Febucci.UI.TextAnimator` is present in the game.
- **[`frontend/src/utils/hookLabels.js`](../frontend/src/utils/hookLabels.js):**
  - Added display label mapping for `textanimator` (`Febucci TextAnimator` / `حوارات TextAnimator (Febucci)`).

---

## 3. Verification

- `cargo test --manifest-path native/Cargo.toml`: **18 passed, 0 failed** in `khutaf-hook`; **3 passed, 0 failed** in `khutaf-proto`.
- `scripts/build-native.ps1`: release build succeeded and updated `runtime/khutaf-hook64.dll`.
- `go test ./...`: all Go test packages passed.
