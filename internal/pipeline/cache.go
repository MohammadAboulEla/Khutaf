//go:build windows

package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sqliteOK        = 0
	sqliteRow       = 100
	sqliteDone      = 101
	sqliteTransient = ^uintptr(0)
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

type sqliteLib struct {
	dll             *syscall.LazyDLL
	procOpen16      *syscall.LazyProc
	procCloseV2     *syscall.LazyProc
	procExec        *syscall.LazyProc
	procErrmsg      *syscall.LazyProc
	procPrepareV2   *syscall.LazyProc
	procBindText    *syscall.LazyProc
	procBindInt64   *syscall.LazyProc
	procStep        *syscall.LazyProc
	procColumnText  *syscall.LazyProc
	procColumnBytes *syscall.LazyProc
	procColumnInt64 *syscall.LazyProc
	procFinalize    *syscall.LazyProc
}

var (
	sqliteOnce sync.Once
	sqliteAPI  *sqliteLib
	sqliteErr  error
)

func loadSQLite() (*sqliteLib, error) {
	sqliteOnce.Do(func() {
		dll := syscall.NewLazyDLL("winsqlite3.dll")
		if err := dll.Load(); err != nil {
			sqliteErr = fmt.Errorf("load winsqlite3.dll: %w", err)
			return
		}
		lib := &sqliteLib{dll: dll}
		procs := map[string]**syscall.LazyProc{
			"sqlite3_open16":       &lib.procOpen16,
			"sqlite3_close_v2":     &lib.procCloseV2,
			"sqlite3_exec":         &lib.procExec,
			"sqlite3_errmsg":       &lib.procErrmsg,
			"sqlite3_prepare_v2":   &lib.procPrepareV2,
			"sqlite3_bind_text":    &lib.procBindText,
			"sqlite3_bind_int64":   &lib.procBindInt64,
			"sqlite3_step":         &lib.procStep,
			"sqlite3_column_text":  &lib.procColumnText,
			"sqlite3_column_bytes": &lib.procColumnBytes,
			"sqlite3_column_int64": &lib.procColumnInt64,
			"sqlite3_finalize":     &lib.procFinalize,
		}
		for name, dst := range procs {
			p := dll.NewProc(name)
			if err := p.Find(); err != nil {
				sqliteErr = fmt.Errorf("winsqlite3.dll missing %s: %w", name, err)
				return
			}
			*dst = p
		}
		sqliteAPI = lib
	})
	return sqliteAPI, sqliteErr
}

// Cache provides 0ms in-memory translation lookup backed by SQLite persistence.
type Cache struct {
	mu     sync.RWMutex
	api    *sqliteLib
	dbMu   sync.Mutex
	db     uintptr
	mem    map[cacheKey]CacheEntry
	manual map[manualKey]string
	shared map[sharedKey]string
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

// OpenCache opens (or creates) a persistent SQLite cache at dbPath and preloads
// all entries into memory for 0ms reads.
func OpenCache(dbPath string) (*Cache, error) {
	if strings.TrimSpace(dbPath) == "" {
		return NewMemoryCache(), nil
	}
	api, err := loadSQLite()
	if err != nil {
		return nil, err
	}
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create cache dir: %w", err)
		}
	}
	pathUTF16, err := windows.UTF16PtrFromString(dbPath)
	if err != nil {
		return nil, err
	}
	var db uintptr
	r, _, _ := api.procOpen16.Call(uintptr(unsafe.Pointer(pathUTF16)), uintptr(unsafe.Pointer(&db)))
	runtime.KeepAlive(pathUTF16)
	if int32(r) != sqliteOK || db == 0 {
		msg := "sqlite3_open16 failed"
		if db != 0 {
			msg = api.lastErr(db)
			api.procCloseV2.Call(db)
		}
		return nil, fmt.Errorf("open %s: %s (%d)", dbPath, msg, int32(r))
	}

	c := &Cache{
		api:    api,
		db:     db,
		mem:    make(map[cacheKey]CacheEntry),
		manual: make(map[manualKey]string),
		shared: make(map[sharedKey]string),
	}
	const schemaSQL = `
PRAGMA busy_timeout=5000;
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS translations (
	game_uid    TEXT NOT NULL,
	provider    TEXT NOT NULL,
	source_text TEXT NOT NULL,
	translated  TEXT NOT NULL,
	manual      INTEGER NOT NULL DEFAULT 0,
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (game_uid, provider, source_text)
);`
	if err := c.exec(schemaSQL); err != nil {
		_ = c.Close()
		return nil, err
	}
	if err := c.preload(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
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
	if c.db != 0 && c.api != nil {
		r, _, _ := c.api.procCloseV2.Call(c.db)
		c.db = 0
		if int32(r) != sqliteOK {
			return fmt.Errorf("sqlite3_close_v2: code %d", int32(r))
		}
	}
	return nil
}

// Get looks up a cached translation, preferring manual overrides before
// provider-specific entries, falling back to "global" and any prior game.
func (c *Cache) Get(gameUID, provider, sourceText string) (string, bool) {
	if c == nil {
		return "", false
	}
	g := NormalizeGameUID(gameUID)
	c.mu.RLock()
	defer c.mu.RUnlock()

	if val, ok := c.manual[manualKey{gameUID: g, sourceText: sourceText}]; ok {
		return val, true
	}
	if g != "global" {
		if val, ok := c.manual[manualKey{gameUID: "global", sourceText: sourceText}]; ok {
			return val, true
		}
	}
	if entry, ok := c.mem[cacheKey{gameUID: g, provider: provider, sourceText: sourceText}]; ok {
		return entry.Translated, true
	}
	if g != "global" {
		if entry, ok := c.mem[cacheKey{gameUID: "global", provider: provider, sourceText: sourceText}]; ok {
			return entry.Translated, true
		}
	}
	if val, ok := c.shared[sharedKey{provider: provider, sourceText: sourceText}]; ok {
		return val, true
	}
	return "", false
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
		c.dbMu.Lock()
		if c.db != 0 {
			_ = c.execUpsertAutoLocked(g, provider, sourceText, translated, now)
		}
		c.dbMu.Unlock()
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
			if includeManual {
				_ = c.exec("DELETE FROM translations;")
			} else {
				_ = c.exec("DELETE FROM translations WHERE manual = 0;")
			}
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

func (c *Cache) preload() error {
	stmt, err := c.prepare("SELECT game_uid, provider, source_text, translated, manual, updated_at FROM translations;")
	if err != nil {
		return err
	}
	defer c.finalize(stmt)

	for {
		r, _, _ := c.api.procStep.Call(stmt)
		switch int32(r) {
		case sqliteRow:
			entry := CacheEntry{
				GameUID:    c.colText(stmt, 0),
				Provider:   c.colText(stmt, 1),
				SourceText: c.colText(stmt, 2),
				Translated: c.colText(stmt, 3),
				Manual:     c.colInt64(stmt, 4) != 0,
				UpdatedAt:  c.colInt64(stmt, 5),
			}
			c.mem[cacheKey{gameUID: entry.GameUID, provider: entry.Provider, sourceText: entry.SourceText}] = entry
		case sqliteDone:
			c.rebuildManualIndexLocked()
			c.rebuildSharedIndexLocked()
			return nil
		default:
			return fmt.Errorf("sqlite3_step preload: %s (%d)", c.api.lastErr(c.db), int32(r))
		}
	}
}

func (c *Cache) exec(sql string) error {
	b := append([]byte(sql), 0)
	r, _, _ := c.api.procExec.Call(c.db, uintptr(unsafe.Pointer(&b[0])), 0, 0, 0)
	runtime.KeepAlive(b)
	if int32(r) != sqliteOK {
		return fmt.Errorf("sqlite3_exec: %s (%d)", c.api.lastErr(c.db), int32(r))
	}
	return nil
}

func (c *Cache) prepare(sql string) (uintptr, error) {
	b := append([]byte(sql), 0)
	var stmt uintptr
	r, _, _ := c.api.procPrepareV2.Call(c.db, uintptr(unsafe.Pointer(&b[0])), uintptr(len(sql)), uintptr(unsafe.Pointer(&stmt)), 0)
	runtime.KeepAlive(b)
	if int32(r) != sqliteOK || stmt == 0 {
		return 0, fmt.Errorf("sqlite3_prepare_v2: %s (%d)", c.api.lastErr(c.db), int32(r))
	}
	return stmt, nil
}

func (c *Cache) finalize(stmt uintptr) {
	if stmt != 0 {
		c.api.procFinalize.Call(stmt)
	}
}

func (c *Cache) bindText(stmt uintptr, idx int32, val string) error {
	var ptr uintptr
	var n int
	var buf []byte
	var empty byte
	if val == "" {
		ptr = uintptr(unsafe.Pointer(&empty))
		n = 0
	} else {
		buf = []byte(val)
		ptr = uintptr(unsafe.Pointer(&buf[0]))
		n = len(buf)
	}
	r, _, _ := c.api.procBindText.Call(stmt, uintptr(idx), ptr, uintptr(n), sqliteTransient)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(&empty)
	if int32(r) != sqliteOK {
		return fmt.Errorf("sqlite3_bind_text(%d): %s", idx, c.api.lastErr(c.db))
	}
	return nil
}

func (c *Cache) bindInt64(stmt uintptr, idx int32, val int64) error {
	r, _, _ := c.api.procBindInt64.Call(stmt, uintptr(idx), uintptr(val))
	if int32(r) != sqliteOK {
		return fmt.Errorf("sqlite3_bind_int64(%d): %s", idx, c.api.lastErr(c.db))
	}
	return nil
}

func (c *Cache) stepDone(stmt uintptr) error {
	r, _, _ := c.api.procStep.Call(stmt)
	if int32(r) != sqliteDone {
		return fmt.Errorf("sqlite3_step: %s (%d)", c.api.lastErr(c.db), int32(r))
	}
	return nil
}

func (c *Cache) colText(stmt uintptr, col int32) string {
	ptr, _, _ := c.api.procColumnText.Call(stmt, uintptr(col))
	if ptr == 0 {
		return ""
	}
	n, _, _ := c.api.procColumnBytes.Call(stmt, uintptr(col))
	byteLen := int(int32(n))
	if byteLen <= 0 {
		return ""
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&ptr))
	return string(unsafe.Slice((*byte)(p), byteLen))
}

func (c *Cache) colInt64(stmt uintptr, col int32) int64 {
	r, _, _ := c.api.procColumnInt64.Call(stmt, uintptr(col))
	return int64(r)
}

func (a *sqliteLib) lastErr(db uintptr) string {
	if db == 0 || a == nil {
		return ""
	}
	ptr, _, _ := a.procErrmsg.Call(db)
	if ptr == 0 {
		return ""
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&ptr))
	return windows.BytePtrToString((*byte)(p))
}

func (c *Cache) execUpsertAutoLocked(gameUID, provider, sourceText, translated string, updatedAt int64) error {
	const q = `
INSERT INTO translations (game_uid, provider, source_text, translated, manual, updated_at)
VALUES (?1, ?2, ?3, ?4, 0, ?5)
ON CONFLICT(game_uid, provider, source_text) DO UPDATE SET
	translated = excluded.translated,
	updated_at = excluded.updated_at
WHERE translations.manual = 0;`
	stmt, err := c.prepare(q)
	if err != nil {
		return err
	}
	defer c.finalize(stmt)
	if err := c.bindText(stmt, 1, gameUID); err != nil {
		return err
	}
	if err := c.bindText(stmt, 2, provider); err != nil {
		return err
	}
	if err := c.bindText(stmt, 3, sourceText); err != nil {
		return err
	}
	if err := c.bindText(stmt, 4, translated); err != nil {
		return err
	}
	if err := c.bindInt64(stmt, 5, updatedAt); err != nil {
		return err
	}
	return c.stepDone(stmt)
}

func (c *Cache) execUpsertManualLocked(gameUID, provider, sourceText, translated string, updatedAt int64) error {
	const q = `
INSERT INTO translations (game_uid, provider, source_text, translated, manual, updated_at)
VALUES (?1, ?2, ?3, ?4, 1, ?5)
ON CONFLICT(game_uid, provider, source_text) DO UPDATE SET
	translated = excluded.translated,
	manual     = 1,
	updated_at = excluded.updated_at;`
	stmt, err := c.prepare(q)
	if err != nil {
		return err
	}
	defer c.finalize(stmt)
	if err := c.bindText(stmt, 1, gameUID); err != nil {
		return err
	}
	if err := c.bindText(stmt, 2, provider); err != nil {
		return err
	}
	if err := c.bindText(stmt, 3, sourceText); err != nil {
		return err
	}
	if err := c.bindText(stmt, 4, translated); err != nil {
		return err
	}
	if err := c.bindInt64(stmt, 5, updatedAt); err != nil {
		return err
	}
	return c.stepDone(stmt)
}

func (c *Cache) execDeleteLocked(gameUID, provider, sourceText string) error {
	stmt, err := c.prepare(`DELETE FROM translations WHERE game_uid = ?1 AND provider = ?2 AND source_text = ?3;`)
	if err != nil {
		return err
	}
	defer c.finalize(stmt)
	if err := c.bindText(stmt, 1, gameUID); err != nil {
		return err
	}
	if err := c.bindText(stmt, 2, provider); err != nil {
		return err
	}
	if err := c.bindText(stmt, 3, sourceText); err != nil {
		return err
	}
	return c.stepDone(stmt)
}

func (c *Cache) execClearScopeLocked(gameUID string, includeManual bool) error {
	q := `DELETE FROM translations WHERE game_uid = ?1 AND manual = 0;`
	if includeManual {
		q = `DELETE FROM translations WHERE game_uid = ?1;`
	}
	stmt, err := c.prepare(q)
	if err != nil {
		return err
	}
	defer c.finalize(stmt)
	if err := c.bindText(stmt, 1, gameUID); err != nil {
		return err
	}
	return c.stepDone(stmt)
}
