package pipeline

import "testing"

func runManualIsolationChecks(t *testing.T, makeCache func(t *testing.T) *Cache, reopen func(t *testing.T, c *Cache) *Cache) {
	t.Helper()
	gameA := `C:\Games\Alpha\Alpha.exe`
	gameB := `C:\Games\Beta\Beta.exe`

	c := makeCache(t)
	if err := c.PutManual(gameA, "manual", "Attack", "هجوم"); err != nil {
		t.Fatalf("PutManual: %v", err)
	}
	c = reopen(t, c)
	defer func() { _ = c.Close() }()

	got, ok := c.Get(gameA, "bing", "Attack")
	if !ok || got != "هجوم" {
		t.Fatalf("gameA manual override Get = (%q, %v), want (%q, true)", got, ok, "هجوم")
	}
	if leaked, ok := c.Get(gameB, "bing", "Attack"); ok {
		t.Fatalf("gameA manual override leaked to gameB: %q", leaked)
	}
}

func runPutAutoRespectsManualChecks(t *testing.T, makeCache func(t *testing.T) *Cache, reopen func(t *testing.T, c *Cache) *Cache) {
	t.Helper()
	gameA := `C:\Games\Alpha\Alpha.exe`
	gameB := `C:\Games\Beta\Beta.exe`

	c := makeCache(t)
	if err := c.PutManual(gameA, "manual", "Start", "ابدأ"); err != nil {
		t.Fatalf("PutManual gameA: %v", err)
	}
	c.PutAuto(gameA, "bing", "Start", "بداية")
	if err := c.PutManual("global", "manual", "Continue", "متابعة"); err != nil {
		t.Fatalf("PutManual global: %v", err)
	}

	c = reopen(t, c)
	defer func() { _ = c.Close() }()

	if got, ok := c.Get(gameA, "bing", "Start"); !ok || got != "ابدأ" {
		t.Fatalf("gameA Get after PutAuto = (%q, %v), want (%q, true)", got, ok, "ابدأ")
	}
	// gameB did not get a manual override; PutAuto returned early so gameB has no entry.
	if got, ok := c.Get(gameB, "bing", "Start"); ok {
		t.Fatalf("expected no entry for gameB after manual-guarded PutAuto, got %q", got)
	}
	if got, ok := c.Get(gameB, "bing", "Continue"); !ok || got != "متابعة" {
		t.Fatalf("global manual fallback for gameB = (%q, %v), want (%q, true)", got, ok, "متابعة")
	}
}

func TestCacheManualOverrideIsolatedPerGameMemory(t *testing.T) {
	runManualIsolationChecks(t,
		func(*testing.T) *Cache { return NewMemoryCache() },
		func(_ *testing.T, c *Cache) *Cache { return c },
	)
}

func TestCachePutAutoRespectsPutManualAcrossProvidersMemory(t *testing.T) {
	runPutAutoRespectsManualChecks(t,
		func(*testing.T) *Cache { return NewMemoryCache() },
		func(_ *testing.T, c *Cache) *Cache { return c },
	)
}
