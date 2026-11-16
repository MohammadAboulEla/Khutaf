package translator

import (
	"context"
	"sync"

	"khutaf/internal/config"
)

// Manager holds the active provider and an in-memory translation cache.
// TODO: replace the memory cache with the SQLite translation memory (Milestone 4).
type Manager struct {
	mu       sync.RWMutex
	provider Provider
	cache    map[string]string
}

// NewManager creates an empty manager.
func NewManager() *Manager { return &Manager{cache: map[string]string{}} }

// Configure (re)builds the active provider from settings and clears the cache.
func (m *Manager) Configure(cfg config.TranslatorSettings) {
	var p Provider
	switch cfg.Active {
	case "bing":
		p = NewBing(cfg.Bing)
	case "llm":
		p = NewLLM(cfg.LLM)
	}
	m.mu.Lock()
	m.provider = p
	m.cache = map[string]string{}
	m.mu.Unlock()
}

// Active returns the provider name, or "" when none is set.
func (m *Manager) Active() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.provider == nil {
		return ""
	}
	return m.provider.Name()
}

// Translate returns a cached translation or queries the provider.
// The bool result reports a cache hit.
func (m *Manager) Translate(ctx context.Context, text string) (string, bool, error) {
	m.mu.RLock()
	p := m.provider
	cached, hit := m.cache[text]
	m.mu.RUnlock()
	if hit {
		return cached, true, nil
	}
	if p == nil {
		return "", false, ErrNoProvider
	}
	out, err := p.Translate(ctx, text)
	if err != nil {
		return "", false, err
	}
	m.mu.Lock()
	m.cache[text] = out
	m.mu.Unlock()
	return out, false, nil
}
