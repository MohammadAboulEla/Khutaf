//go:build windows

package pipeline

import (
	"path/filepath"
	"testing"
)

func TestCacheManualOverrideIsolatedPerGameSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	runManualIsolationChecks(t,
		func(t *testing.T) *Cache {
			c, err := OpenCache(dbPath)
			if err != nil {
				t.Fatalf("OpenCache: %v", err)
			}
			return c
		},
		func(t *testing.T, c *Cache) *Cache {
			if err := c.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			reopened, err := OpenCache(dbPath)
			if err != nil {
				t.Fatalf("reopen OpenCache: %v", err)
			}
			return reopened
		},
	)
}

func TestCachePutAutoRespectsPutManualAcrossProvidersSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	runPutAutoRespectsManualChecks(t,
		func(t *testing.T) *Cache {
			c, err := OpenCache(dbPath)
			if err != nil {
				t.Fatalf("OpenCache: %v", err)
			}
			return c
		},
		func(t *testing.T, c *Cache) *Cache {
			if err := c.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			reopened, err := OpenCache(dbPath)
			if err != nil {
				t.Fatalf("reopen OpenCache: %v", err)
			}
			return reopened
		},
	)
}

func TestSQLiteCachePersistenceAndManualOverride(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache.db")
	c, err := OpenCache(dbPath)
	if err != nil {
		t.Fatalf("OpenCache: %v", err)
	}

	const game = `E:\Games\Wonderia.exe`
	c.PutAuto(game, "bing", "Hello", "مرحبا")
	c.PutAuto(game, "bing", "Goodbye", "وداعا")
	if err := c.PutManual(game, "bing", "Hello", "أهلاً بك"); err != nil {
		t.Fatalf("PutManual: %v", err)
	}
	// PutAuto must not overwrite a manual override.
	c.PutAuto(game, "bing", "Hello", "مرحبا مجددا")

	if got, ok := c.Get(game, "bing", "Hello"); !ok || got != "أهلاً بك" {
		t.Fatalf("Get before reopen = (%q, %v)", got, ok)
	}
	// Manual override also applies when switching providers for the same game.
	if got, ok := c.Get(game, "llm:gpt-4o-mini", "Hello"); !ok || got != "أهلاً بك" {
		t.Fatalf("Get cross-provider = (%q, %v)", got, ok)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen from disk to verify persistence across restarts.
	c2, err := OpenCache(dbPath)
	if err != nil {
		t.Fatalf("OpenCache reopen: %v", err)
	}
	defer c2.Close()

	if c2.Count() != 2 {
		t.Fatalf("Count after reopen = %d, want 2", c2.Count())
	}
	if got, ok := c2.Get(game, "bing", "Hello"); !ok || got != "أهلاً بك" {
		t.Fatalf("Get after reopen = (%q, %v)", got, ok)
	}
	if got, ok := c2.Get(game, "bing", "Goodbye"); !ok || got != "وداعا" {
		t.Fatalf("Get auto after reopen = (%q, %v)", got, ok)
	}

	// Clear auto entries while preserving manual overrides.
	removed := c2.Clear("", false)
	if removed != 1 {
		t.Fatalf("Clear removed = %d, want 1", removed)
	}
	if _, ok := c2.Get(game, "bing", "Goodbye"); ok {
		t.Fatal("expected auto entry to be cleared")
	}
	if got, ok := c2.Get(game, "bing", "Hello"); !ok || got != "أهلاً بك" {
		t.Fatalf("expected manual entry preserved, got (%q, %v)", got, ok)
	}

	if err := c2.Delete(game, "bing", "Hello"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if c2.Count() != 0 {
		t.Fatalf("Count after Delete = %d, want 0", c2.Count())
	}

	// Cross-game fallback: Game B reuses Game A's translation unless Game B overrides it.
	const gameA = `C:\Games\GameA\a.exe`
	const gameB = `C:\Games\GameB\b.exe`
	c2.PutAuto(gameA, "bing", "Why?", "لماذا؟")
	if got, ok := c2.Get(gameB, "bing", "Why?"); !ok || got != "لماذا؟" {
		t.Fatalf("Get cross-game fallback = (%q, %v), want لماذا؟", got, ok)
	}
	c2.PutAuto(gameB, "bing", "Why?", "لمَ؟")
	if got, ok := c2.Get(gameB, "bing", "Why?"); !ok || got != "لمَ؟" {
		t.Fatalf("Get game-specific override = (%q, %v), want لمَ؟", got, ok)
	}
}
