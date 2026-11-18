//go:build windows

package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"khutaf/internal/config"
)

func TestRuleSetApply(t *testing.T) {
	rs, err := Compile([]config.Rule{
		{Enabled: true, Regex: true, Pattern: `\s+`, Replace: " "},
		{Enabled: true, Pattern: "foo", Replace: "bar"},
		{Enabled: false, Pattern: "bar", Replace: "baz"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rs.Apply("foo   x\n y"); got != "bar x y" {
		t.Fatalf("Apply = %q", got)
	}
}

func TestCompileRejectsBadRegex(t *testing.T) {
	if _, err := Compile([]config.Rule{{Enabled: true, Regex: true, Pattern: "("}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPlaceholdersPreserved(t *testing.T) {
	if !PlaceholdersPreserved("Hi [name], {0} gold", "مرحبا [name]، {0} ذهب") {
		t.Fatal("expected preserved")
	}
	if PlaceholdersPreserved("Hi [name]", "مرحبا") {
		t.Fatal("expected missing placeholder to fail")
	}
}

func TestGlossaryApplyAndPromptContext(t *testing.T) {
	global := []config.GlossaryEntry{
		{Enabled: true, Source: "Key", Target: "مفتاح"},
		{Enabled: true, Source: "Wonderia", Target: "وونديريا"},
		{Enabled: false, Source: "Disabled", Target: "معطل"},
	}
	game := []config.GlossaryEntry{
		{Enabled: true, Source: "Silver Key", Target: "المفتاح الفضي"},
		{Enabled: true, Source: "wonderia", Target: "عالم وونديريا", CaseSensitive: true},
	}

	g := CompileGlossary(global, game)

	// Word-boundary and longest-first priority: "Silver Key" before "Key", and "Monkey" untouched.
	got := g.Apply("Find the silver key in Monkey Island, not wonderia or Wonderia.")
	want := "Find the المفتاح الفضي in Monkey Island, not عالم وونديريا or Wonderia."
	if got != want {
		t.Fatalf("Glossary.Apply = %q, want %q", got, want)
	}

	prompt := g.PromptContext("Take the Silver Key to wonderia.")
	if !strings.Contains(prompt, "Silver Key -> المفتاح الفضي") {
		t.Fatalf("PromptContext missing Silver Key: %q", prompt)
	}
	if !strings.Contains(prompt, "wonderia -> عالم وونديريا") {
		t.Fatalf("PromptContext missing game override: %q", prompt)
	}
	if strings.Contains(prompt, "Disabled") {
		t.Fatalf("PromptContext included disabled entry: %q", prompt)
	}
	if got := g.PromptContext("Monkey"); got != "" {
		t.Fatalf("PromptContext matched partial word unexpectedly: %q", got)
	}
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
