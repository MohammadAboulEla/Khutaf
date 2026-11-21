//go:build windows

package session

import (
	"testing"

	"khutaf/internal/config"
)

func TestGameOverridesResolutionAndDetachReversion(t *testing.T) {
	store := config.NewMemoryStore()
	cfg := store.Config()
	cfg.Embed.MaxLineLength = 42
	cfg.Embed.FontFamily = "Tahoma"
	cfg.Translator.Active = "bing"
	cfg.Translator.FallbackProvider = "google"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	s := New(store, "", nil)
	defer s.Close()

	exe := `C:\Games\RPG\RPG.exe`

	// Initially no overrides exist -> inherits global.
	if got := s.effectiveEmbed(cfg.Embed, exe); got.MaxLineLength != 42 || got.FontFamily != "Tahoma" {
		t.Fatalf("expected global embed defaults, got %+v", got)
	}
	if got := s.effectiveTranslator(cfg.Translator, exe); got.Active != "bing" || got.FallbackProvider != "google" {
		t.Fatalf("expected global translator defaults, got %+v", got)
	}

	// Save per-game overrides while RPG.exe is attached.
	s.mu.Lock()
	s.gamePath = exe
	s.mu.Unlock()

	eo := cfg.Embed
	eo.MaxLineLength = 26
	eo.FontFamily = "Arial"
	to := cfg.Translator
	to.Active = "yandex"
	to.FallbackProvider = "bing"

	if err := s.SaveGameOverrides(exe, &eo, &to); err != nil {
		t.Fatalf("SaveGameOverrides: %v", err)
	}

	if got := s.embedCfg.Load(); got.MaxLineLength != 26 || got.FontFamily != "Arial" {
		t.Fatalf("expected active embedCfg override, got %+v", got)
	}
	if s.translator.Active() != "yandex" || s.translator.Fallback() != "bing" {
		t.Fatalf("expected active translator yandex/bing, got %s/%s", s.translator.Active(), s.translator.Fallback())
	}

	// Detach must revert active embedCfg and translator back to global settings.
	s.Detach()
	if got := s.embedCfg.Load(); got.MaxLineLength != 42 || got.FontFamily != "Tahoma" {
		t.Fatalf("expected Detach to revert embedCfg to global, got %+v", got)
	}
	if s.translator.Active() != "bing" || s.translator.Fallback() != "google" {
		t.Fatalf("expected Detach to revert translator to bing/google, got %s/%s", s.translator.Active(), s.translator.Fallback())
	}
}
