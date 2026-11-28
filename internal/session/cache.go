//go:build windows

package session

import (
	"errors"
	"fmt"
	"strings"

	"khutaf/internal/pipeline"
)

// ClearTranslationCache clears non-manual cached translations and returns the count cleared.
func (s *Session) ClearTranslationCache() int {
	n := s.translator.ClearCache()
	s.log("info", fmt.Sprintf("cleared %d cached translations", n))
	return n
}

// CacheCount returns the total number of cached translations.
func (s *Session) CacheCount() int { return s.translator.CacheSize() }

// ListCacheEntries returns filtered cache entries for the cache browser UI.
func (s *Session) ListCacheEntries(gamePath, query string, manualOnly bool, limit int) []pipeline.CacheEntry {
	return s.translator.Cache().List(gamePath, query, manualOnly, limit)
}

// SaveCacheEntry inserts or updates a persistent manual translation override.
func (s *Session) SaveCacheEntry(gameUID, provider, sourceText, translated string) error {
	if strings.TrimSpace(sourceText) == "" {
		return errors.New("source text is required")
	}
	return s.translator.Cache().PutManual(gameUID, provider, sourceText, translated)
}

// DeleteCacheEntry deletes a single entry from the SQLite translation cache.
func (s *Session) DeleteCacheEntry(gameUID, provider, sourceText string) error {
	return s.translator.Cache().Delete(gameUID, provider, sourceText)
}

// ClearScopeCache clears cached entries for gamePath (or all games when empty).
func (s *Session) ClearScopeCache(gamePath string, includeManual bool) int {
	n := s.translator.Cache().Clear(gamePath, includeManual)
	s.log("info", fmt.Sprintf("cleared %d cached translations", n))
	return n
}
