//go:build !windows

package pipeline

import (
	"errors"
	"strings"
)

type sqliteLib struct{}

// OpenCache opens an in-memory cache when dbPath is empty; persistent SQLite requires Windows.
func OpenCache(dbPath string) (*Cache, error) {
	if strings.TrimSpace(dbPath) == "" {
		return NewMemoryCache(), nil
	}
	return nil, errors.New("sqlite cache persistence requires Windows")
}

func (c *Cache) closeDBLocked() error {
	c.db = 0
	return nil
}

func (c *Cache) execUpsertAutoLocked(string, string, string, string, int64) error { return nil }
func (c *Cache) execUpsertManualLocked(string, string, string, string, int64) error { return nil }
func (c *Cache) execDeleteLocked(string, string, string) error                       { return nil }
func (c *Cache) execClearAllLocked(bool) error                                       { return nil }
func (c *Cache) execClearScopeLocked(string, bool) error                             { return nil }
