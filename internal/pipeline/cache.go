package pipeline

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CacheEntry represents one persisted translation record in cache.db.
type CacheEntry struct {
	GameUID    string `json:"gameUid"`
	Provider   string `json:"provider"`
	SourceText string `json:"sourceText"`
	Translated string `json:"translated"`
	Manual     bool   `json:"manual"`
	UpdatedAt  int64  `json:"updatedAt"`
}

type cacheKey struct {
	gameUID    string
	provider   string
	sourceText string
}

type manualKey struct {
	gameUID    string
	sourceText string
}

type sharedKey struct {
	provider   string
	sourceText string
}

// TelemetryFunc receives structured telemetry events from the cache.
type TelemetryFunc func(subsys, fnName string, durUs int64, level, msg string, fields map[string]any)

// Cache provides 0ms in-memory translation lookup backed by SQLite persistence.
type Cache struct {
	mu     sync.RWMutex
	api    *sqliteLib
	dbMu   sync.Mutex
	db     uintptr
	mem    map[cacheKey]CacheEntry
	manual map[manualKey]string
	shared map[sharedKey]string
	telem  atomic.Pointer[TelemetryFunc]
}

// SetTelemetry registers an optional telemetry callback.
func (c *Cache) SetTelemetry(fn TelemetryFunc) {
	if c == nil {
		return
	}
	if fn == nil {
		c.telem.Store(nil)
		return
	}
	c.telem.Store(&fn)
}

func (c *Cache) emitTelem(fnName string, durUs int64, level, msg string, fields map[string]any) {
	if p := c.telem.Load(); p != nil && *p != nil {
		(*p)("PIPELINE_CACHE", fnName, durUs, level, msg, fields)
	}
}

// NormalizeGameUID normalizes a game executable path into a cache scope key.
func NormalizeGameUID(gamePath string) string {
	trimmed := strings.TrimSpace(gamePath)
	if trimmed == "" || strings.EqualFold(trimmed, "global") {
		return "global"
	}
	return strings.ToLower(filepath.Clean(trimmed))
}

// NewMemoryCache creates an in-memory-only translation cache.
func NewMemoryCache() *Cache {
	return &Cache{
		mem:    make(map[cacheKey]CacheEntry),
		manual: make(map[manualKey]string),
		shared: make(map[sharedKey]string),
	}
}

// Close closes the underlying SQLite connection.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dbMu.Lock()
	defer c.dbMu.Unlock()
	return c.closeDBLocked()
}

// Get looks up a cached translation, preferring manual overrides before
// provider-specific entries, falling back to "global" and any prior game.
func (c *Cache) Get(gameUID, provider, sourceText string) (string, bool) {
	if c == nil {
		return "", false
	}
	start := time.Now()
	val, tier := c.getWithTier(gameUID, provider, sourceText)
	if tier == "" {
		c.emitTelem("Cache.Get", time.Since(start).Microseconds(), "trace", "miss", map[string]any{"tier": "miss"})
		return "", false
	}
	c.emitTelem("Cache.Get", time.Since(start).Microseconds(), "trace", "hit:"+tier, map[string]any{"tier": tier})
	return val, true
}

// GetSilent performs the same cache lookup as Get without emitting telemetry.
func (c *Cache) GetSilent(gameUID, provider, sourceText string) (string, bool) {
	if c == nil {
		return "", false
	}
	val, tier := c.getWithTier(gameUID, provider, sourceText)
	return val, tier != ""
}

func (c *Cache) getWithTier(gameUID, provider, sourceText string) (string, string) {
	g := NormalizeGameUID(gameUID)
	c.mu.RLock()
	defer c.mu.RUnlock()

	if v, ok := c.manual[manualKey{gameUID: g, sourceText: sourceText}]; ok {
		return v, "manual_game"
	}
	if g != "global" {
		if v, ok := c.manual[manualKey{gameUID: "global", sourceText: sourceText}]; ok {
			return v, "manual_global"
		}
	}
	if entry, ok := c.mem[cacheKey{gameUID: g, provider: provider, sourceText: sourceText}]; ok {
		return entry.Translated, "auto_game"
	}
	if g != "global" {
		if entry, ok := c.mem[cacheKey{gameUID: "global", provider: provider, sourceText: sourceText}]; ok {
			return entry.Translated, "auto_global"
		}
	}
	if v, ok := c.shared[sharedKey{provider: provider, sourceText: sourceText}]; ok {
		return v, "shared"
	}
	return "", ""
}

// PutAuto stores an automatic translation without overwriting manual overrides.
func (c *Cache) PutAuto(gameUID, provider, sourceText, translated string) {
	if c == nil {
		return
	}
	g := NormalizeGameUID(gameUID)
	now := time.Now().Unix()

	c.mu.Lock()
	if _, hasManual := c.manual[manualKey{gameUID: g, sourceText: sourceText}]; hasManual {
		c.mu.Unlock()
		return
	}
	k := cacheKey{gameUID: g, provider: provider, sourceText: sourceText}
	if existing, ok := c.mem[k]; ok && existing.Manual {
		c.mu.Unlock()
		return
	}
	c.mem[k] = CacheEntry{
		GameUID:    g,
		Provider:   provider,
		SourceText: sourceText,
		Translated: translated,
		Manual:     false,
		UpdatedAt:  now,
	}
	c.shared[sharedKey{provider: provider, sourceText: sourceText}] = translated
	hasDB := c.db != 0
	c.mu.Unlock()

	if hasDB {
		t0 := time.Now()
		c.dbMu.Lock()
		var dbErr error
		if c.db != 0 {
			dbErr = c.execUpsertAutoLocked(g, provider, sourceText, translated, now)
		}
		c.dbMu.Unlock()
		durUs := time.Since(t0).Microseconds()
		if dbErr != nil {
			c.emitTelem("Cache.PutAuto", durUs, "error", dbErr.Error(), nil)
		} else if durUs > 50_000 {
			c.emitTelem("Cache.PutAuto", durUs, "warn", "slow sqlite write", nil)
		}
	}
}

// PutManual stores or updates a user-edited manual translation override.
func (c *Cache) PutManual(gameUID, provider, sourceText, translated string) error {
	if c == nil {
		return errors.New("cache unavailable")
	}
	g := NormalizeGameUID(gameUID)
	if strings.TrimSpace(provider) == "" {
		provider = "manual"
	}
	now := time.Now().Unix()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.db != 0 {
		c.dbMu.Lock()
		err := c.execUpsertManualLocked(g, provider, sourceText, translated, now)
		c.dbMu.Unlock()
		if err != nil {
			return err
		}
	}
	k := cacheKey{gameUID: g, provider: provider, sourceText: sourceText}
	c.mem[k] = CacheEntry{
		GameUID:    g,
		Provider:   provider,
		SourceText: sourceText,
		Translated: translated,
		Manual:     true,
		UpdatedAt:  now,
	}
	c.manual[manualKey{gameUID: g, sourceText: sourceText}] = translated
	if g == "global" {
		c.shared[sharedKey{provider: provider, sourceText: sourceText}] = translated
	}
	return nil
}

// Delete removes one cache entry.
func (c *Cache) Delete(gameUID, provider, sourceText string) error {
	if c == nil {
		return nil
	}
	g := NormalizeGameUID(gameUID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.db != 0 {
		c.dbMu.Lock()
		err := c.execDeleteLocked(g, provider, sourceText)
		c.dbMu.Unlock()
		if err != nil {
			return err
		}
	}
	delete(c.mem, cacheKey{gameUID: g, provider: provider, sourceText: sourceText})
	c.refreshManualKeyLocked(g, sourceText)
	c.refreshSharedKeyLocked(provider, sourceText)
	return nil
}

// Clear removes cached translations for all games (when gameUID is "" or "all")
// or a single normalized gameUID, optionally preserving manual overrides.
func (c *Cache) Clear(gameUID string, includeManual bool) int {
	if c == nil {
		return 0
	}
	all := gameUID == "" || strings.EqualFold(strings.TrimSpace(gameUID), "all")
	g := NormalizeGameUID(gameUID)

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := 0
	for k, e := range c.mem {
		if !all && e.GameUID != g {
			continue
		}
		if e.Manual && !includeManual {
			continue
		}
		delete(c.mem, k)
		removed++
	}
	c.rebuildManualIndexLocked()
	c.rebuildSharedIndexLocked()

	if c.db != 0 {
		c.dbMu.Lock()
		if all {
			_ = c.execClearAllLocked(includeManual)
		} else {
			_ = c.execClearScopeLocked(g, includeManual)
		}
		c.dbMu.Unlock()
	}
	return removed
}

// Count returns the total number of cached entries.
func (c *Cache) Count() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.mem)
}

// List returns matching cache entries sorted with manual overrides first, then
// most recently updated.
func (c *Cache) List(gameUID, query string, manualOnly bool, limit int) []CacheEntry {
	if c == nil {
		return nil
	}
	if limit <= 0 {
		limit = 200
	}
	filterGame := ""
	trimmed := strings.TrimSpace(gameUID)
	if trimmed != "" && !strings.EqualFold(trimmed, "global") && !strings.EqualFold(trimmed, "all") {
		filterGame = NormalizeGameUID(trimmed)
	}
	q := strings.ToLower(strings.TrimSpace(query))

	c.mu.RLock()
	out := make([]CacheEntry, 0, len(c.mem))
	for _, e := range c.mem {
		if filterGame != "" && e.GameUID != filterGame {
			continue
		}
		if manualOnly && !e.Manual {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.SourceText), q) && !strings.Contains(strings.ToLower(e.Translated), q) {
			continue
		}
		out = append(out, e)
	}
	c.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Manual != out[j].Manual {
			return out[i].Manual
		}
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].SourceText < out[j].SourceText
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (c *Cache) refreshManualKeyLocked(gameUID, sourceText string) {
	mk := manualKey{gameUID: gameUID, sourceText: sourceText}
	var bestUpdated int64 = -1
	bestVal := ""
	found := false
	for _, e := range c.mem {
		if e.Manual && e.GameUID == gameUID && e.SourceText == sourceText {
			if !found || e.UpdatedAt >= bestUpdated {
				found = true
				bestUpdated = e.UpdatedAt
				bestVal = e.Translated
			}
		}
	}
	if found {
		c.manual[mk] = bestVal
	} else {
		delete(c.manual, mk)
	}
}

func (c *Cache) rebuildManualIndexLocked() {
	c.manual = make(map[manualKey]string)
	bestTs := make(map[manualKey]int64)
	for _, e := range c.mem {
		if !e.Manual {
			continue
		}
		mk := manualKey{gameUID: e.GameUID, sourceText: e.SourceText}
		if prev, exists := bestTs[mk]; !exists || e.UpdatedAt >= prev {
			bestTs[mk] = e.UpdatedAt
			c.manual[mk] = e.Translated
		}
	}
}

func (c *Cache) refreshSharedKeyLocked(provider, sourceText string) {
	sk := sharedKey{provider: provider, sourceText: sourceText}
	var bestUpdated int64 = -1
	bestVal := ""
	found := false
	for _, e := range c.mem {
		if e.Manual && e.GameUID != "global" {
			continue
		}
		if e.Provider == provider && e.SourceText == sourceText {
			if !found || e.UpdatedAt >= bestUpdated {
				found = true
				bestUpdated = e.UpdatedAt
				bestVal = e.Translated
			}
		}
	}
	if found {
		c.shared[sk] = bestVal
	} else {
		delete(c.shared, sk)
	}
}

func (c *Cache) rebuildSharedIndexLocked() {
	c.shared = make(map[sharedKey]string)
	bestTs := make(map[sharedKey]int64)
	for _, e := range c.mem {
		if e.Manual && e.GameUID != "global" {
			continue
		}
		sk := sharedKey{provider: e.Provider, sourceText: e.SourceText}
		if prev, exists := bestTs[sk]; !exists || e.UpdatedAt >= prev {
			bestTs[sk] = e.UpdatedAt
			c.shared[sk] = e.Translated
		}
	}
}
