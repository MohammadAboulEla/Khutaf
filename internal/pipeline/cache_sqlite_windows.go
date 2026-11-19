//go:build windows

package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sqliteOK        = 0
	sqliteRow       = 100
	sqliteDone      = 101
	sqliteTransient = ^uintptr(0)
)

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

func (c *Cache) closeDBLocked() error {
	if c.db != 0 && c.api != nil {
		r, _, _ := c.api.procCloseV2.Call(c.db)
		c.db = 0
		if int32(r) != sqliteOK {
			return fmt.Errorf("sqlite3_close_v2: code %d", int32(r))
		}
	}
	return nil
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

func (c *Cache) execClearAllLocked(includeManual bool) error {
	if includeManual {
		return c.exec("DELETE FROM translations;")
	}
	return c.exec("DELETE FROM translations WHERE manual = 0;")
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
