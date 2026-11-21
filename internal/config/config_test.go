package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestOpenBacksUpCorruptConfigJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"embed": {broken`), 0o644); err != nil {
		t.Fatalf("write corrupt config: %v", err)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatalf("expected Open to recover from corrupt config.json, got: %v", err)
	}
	if st.InitWarning() == "" {
		t.Fatal("expected non-empty InitWarning after corrupt config.json")
	}
	if st.Config().Embed.TimeoutMs != Default().Embed.TimeoutMs {
		t.Fatalf("expected default TimeoutMs %d, got %d", Default().Embed.TimeoutMs, st.Config().Embed.TimeoutMs)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "config.json.bad-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected 1 config.json.bad-* backup file, got %v (err=%v)", matches, err)
	}
}

func TestStoreConcurrentSaveAndUpdate(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				cfg := st.Config()
				cfg.Embed.TimeoutMs = 1000 + id*100 + i
				if err := st.SaveConfig(cfg); err != nil {
					t.Errorf("SaveConfig: %v", err)
					return
				}
				exe := fmt.Sprintf(`C:\Games\Game%d.exe`, id)
				if err := st.UpdateGame(exe, func(gp *GameProfile) {
					gp.Title = fmt.Sprintf("Game-%d-%d", id, i)
					gp.EmbedHooks = []EmbedHook{{HookCode: "UNITY:TMP_Text.set_text", Ctx: uint64(i)}}
				}); err != nil {
					t.Errorf("UpdateGame: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// Reopen from disk to ensure written JSON files are completely valid.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after concurrent writes: %v", err)
	}
	if len(reopened.Games()) != 8 {
		t.Fatalf("expected 8 saved games, got %d", len(reopened.Games()))
	}
	if strings.TrimSpace(reopened.InitWarning()) != "" {
		t.Fatalf("unexpected InitWarning on reopen: %s", reopened.InitWarning())
	}
}

func TestOpenFailsWhenDirNotWritable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if _, err := Open(blocker); err == nil {
		t.Fatal("expected Open to fail when target path is not a writable directory")
	}

	mem := NewMemoryStore()
	mem.SetInitWarning("first warning")
	mem.SetInitWarning("second warning")
	if got := mem.InitWarning(); got != "first warning; second warning" {
		t.Fatalf("InitWarning = %q, want %q", got, "first warning; second warning")
	}
}

func TestAPIKeyDPAPIRoundTripAndMigration(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := st.Config()
	cfg.Translator.LLM.APIKey = "sk-test-secret-12345"
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if got := st.Config().Translator.LLM.APIKey; got != "sk-test-secret-12345" {
		t.Fatalf("in-memory APIKey = %q, want plaintext", got)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(string(raw), `"dpapi:`) || strings.Contains(string(raw), "sk-test-secret-12345") {
			t.Fatalf("expected encrypted dpapi token on disk, got:\n%s", raw)
		}
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Config().Translator.LLM.APIKey; got != "sk-test-secret-12345" {
		t.Fatalf("reopened APIKey = %q, want %q", got, "sk-test-secret-12345")
	}
	if w := reopened.InitWarning(); w != "" {
		t.Fatalf("unexpected InitWarning: %q", w)
	}

	// Legacy plaintext config.json should load cleanly and migrate on next save.
	legacyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacyDir, "config.json"), []byte(`{"translator":{"llm":{"apiKey":"sk-legacy-plain"}}}`), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	legacyStore, err := Open(legacyDir)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	if got := legacyStore.Config().Translator.LLM.APIKey; got != "sk-legacy-plain" {
		t.Fatalf("legacy APIKey = %q, want sk-legacy-plain", got)
	}
	if legacyStore.InitWarning() != "" {
		t.Fatalf("unexpected InitWarning on legacy plaintext: %q", legacyStore.InitWarning())
	}
	if err := legacyStore.SaveConfig(legacyStore.Config()); err != nil {
		t.Fatalf("SaveConfig legacy: %v", err)
	}
	if runtime.GOOS == "windows" {
		migratedRaw, err := os.ReadFile(filepath.Join(legacyDir, "config.json"))
		if err != nil {
			t.Fatalf("read migrated config: %v", err)
		}
		if !strings.Contains(string(migratedRaw), `"dpapi:`) || strings.Contains(string(migratedRaw), "sk-legacy-plain") {
			t.Fatalf("expected legacy secret to be encrypted on save, got:\n%s", migratedRaw)
		}
	}

	// Unreadable dpapi ciphertext must warn and clear the key rather than failing startup.
	badDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(badDir, "config.json"), []byte(`{"translator":{"llm":{"apiKey":"dpapi:bm90LWEtdmFsaWQtYmxvYg=="}}}`), 0o644); err != nil {
		t.Fatalf("write bad dpapi config: %v", err)
	}
	badStore, err := Open(badDir)
	if err != nil {
		t.Fatalf("Open bad dpapi: %v", err)
	}
	if badStore.Config().Translator.LLM.APIKey != "" {
		t.Fatalf("expected unreadable APIKey to be cleared, got %q", badStore.Config().Translator.LLM.APIKey)
	}
	if badStore.InitWarning() == "" {
		t.Fatal("expected InitWarning when dpapi secret fails to decrypt")
	}
}

func TestUISettingsDefaultsAndPersistence(t *testing.T) {
	dir := t.TempDir()

	// Legacy config.json without ui object should inherit UI defaults.
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"embed":{"timeoutMs":2500}}`), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open legacy: %v", err)
	}
	cfg := st.Config()
	if cfg.UI.Language != "ar" || cfg.UI.Mode != "simple" || cfg.UI.Onboarded {
		t.Fatalf("unexpected UI defaults on legacy config: %+v", cfg.UI)
	}

	cfg.UI = UISettings{Language: "en", Mode: "advanced", Onboarded: true}
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.Config().UI
	if got.Language != "en" || got.Mode != "advanced" || !got.Onboarded {
		t.Fatalf("UI settings did not round-trip: %+v", got)
	}
}

func TestGameOverridesAndFallbackPersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cfg := st.Config()
	cfg.Translator.FallbackProvider = "google"
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	exe := `C:\Games\SampleGame\Game.exe`
	eo := Default().Embed
	eo.MaxLineLength = 28
	eo.FontSizeRelative = 0.85
	to := Default().Translator
	to.Active = "llm"
	to.FallbackProvider = "bing"
	to.LLM.APIKey = "sk-game-secret-999"

	if err := st.UpdateGame(exe, func(g *GameProfile) {
		g.Title = "SampleGame"
		g.EmbedOverride = &eo
		g.TranslatorOverride = &to
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	if runtime.GOOS == "windows" {
		raw, err := os.ReadFile(filepath.Join(dir, "games.json"))
		if err != nil {
			t.Fatalf("read games.json: %v", err)
		}
		if !strings.Contains(string(raw), `"dpapi:`) || strings.Contains(string(raw), "sk-game-secret-999") {
			t.Fatalf("expected encrypted per-game API key in games.json, got:\n%s", raw)
		}
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Config().Translator.FallbackProvider != "google" {
		t.Fatalf("FallbackProvider = %q, want google", reopened.Config().Translator.FallbackProvider)
	}
	prof := reopened.Game(exe)
	if prof.EmbedOverride == nil || prof.EmbedOverride.MaxLineLength != 28 || prof.EmbedOverride.FontSizeRelative != 0.85 {
		t.Fatalf("unexpected EmbedOverride: %+v", prof.EmbedOverride)
	}
	if prof.TranslatorOverride == nil || prof.TranslatorOverride.Active != "llm" ||
		prof.TranslatorOverride.FallbackProvider != "bing" ||
		prof.TranslatorOverride.LLM.APIKey != "sk-game-secret-999" {
		t.Fatalf("unexpected TranslatorOverride: %+v", prof.TranslatorOverride)
	}

	// Clearing overrides back to nil must persist cleanly.
	if err := reopened.UpdateGame(exe, func(g *GameProfile) {
		g.EmbedOverride = nil
		g.TranslatorOverride = nil
	}); err != nil {
		t.Fatalf("clear overrides: %v", err)
	}
	prof = reopened.Game(exe)
	if prof.EmbedOverride != nil || prof.TranslatorOverride != nil {
		t.Fatalf("expected nil overrides after clear, got embed=%v trans=%v", prof.EmbedOverride, prof.TranslatorOverride)
	}
}

func TestHotkeysAndAutoAttachPersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	def := st.Config().Hotkeys
	if !def.Enabled || def.MinimizeToTray || def.CycleMode != "Ctrl+Alt+M" || def.ToggleHooksMenu != "Ctrl+Alt+H" {
		t.Fatalf("unexpected default hotkeys: %+v", def)
	}

	cfg := st.Config()
	cfg.Hotkeys.Enabled = false
	cfg.Hotkeys.MinimizeToTray = true
	cfg.Hotkeys.CycleMode = "Ctrl+Shift+M"
	cfg.Hotkeys.TogglePause = ""
	cfg.Hotkeys.ToggleHooksMenu = "Ctrl+Alt+K"
	cfg.Hotkeys.PopupW = 520
	cfg.Hotkeys.PopupH = 410
	cfg.Hotkeys.PopupX = -1200
	cfg.Hotkeys.PopupY = 140
	cfg.Hotkeys.PopupHasPos = true
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	exe := `C:\Games\Watcher\Game.exe`
	if err := st.UpdateGame(exe, func(g *GameProfile) {
		g.Title = "Watcher"
		g.AutoAttach = true
	}); err != nil {
		t.Fatalf("UpdateGame: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	hk := reopened.Config().Hotkeys
	if hk.Enabled || !hk.MinimizeToTray || hk.CycleMode != "Ctrl+Shift+M" || hk.TogglePause != "" || hk.ToggleHooksMenu != "Ctrl+Alt+K" {
		t.Fatalf("reopened hotkeys = %+v", hk)
	}
	if hk.PopupW != 520 || hk.PopupH != 410 || hk.PopupX != -1200 || hk.PopupY != 140 || !hk.PopupHasPos {
		t.Fatalf("reopened popup bounds = (%d,%d,%d,%d,hasPos=%v)", hk.PopupW, hk.PopupH, hk.PopupX, hk.PopupY, hk.PopupHasPos)
	}
	if !reopened.Game(exe).AutoAttach {
		t.Fatal("expected AutoAttach=true to persist")
	}
}
