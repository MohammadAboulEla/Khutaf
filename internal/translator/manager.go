package translator

import (
	"context"
	"net/http"
	"sync"
	"time"

	"khutaf/internal/config"
)

const maxConcurrentRequests = 4

type inflightCall struct {
	done chan struct{}
	val  string
	err  error
}

// Manager holds the active provider, an in-memory translation cache, and
// deduplicates concurrent identical requests.
// TODO: replace the memory cache with the SQLite translation memory (Milestone 4).
type Manager struct {
	mu       sync.RWMutex
	hasCfg   bool
	cfg      config.TranslatorSettings
	provider Provider
	cache    map[string]string
	inflight map[string]*inflightCall
	sem      chan struct{}
}

// NewManager creates an empty manager.
func NewManager() *Manager {
	return &Manager{
		cache:    map[string]string{},
		inflight: map[string]*inflightCall{},
		sem:      make(chan struct{}, maxConcurrentRequests),
	}
}

func newHTTPTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
}

// Configure (re)builds the active provider when translator settings change.
func (m *Manager) Configure(cfg config.TranslatorSettings) {
	m.mu.RLock()
	unchanged := m.hasCfg && m.cfg == cfg
	m.mu.RUnlock()
	if unchanged {
		return
	}

	var p Provider
	switch cfg.Active {
	case "bing":
		p = NewBing(cfg.Bing)
	case "llm":
		p = NewLLM(cfg.LLM)
	}
	m.mu.Lock()
	m.cfg = cfg
	m.hasCfg = true
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
// Concurrent requests for the same text share one in-flight call.
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

	m.mu.Lock()
	if cached, hit = m.cache[text]; hit {
		m.mu.Unlock()
		return cached, true, nil
	}
	if call, exists := m.inflight[text]; exists {
		m.mu.Unlock()
		select {
		case <-call.done:
			return call.val, call.err == nil, call.err
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	m.inflight[text] = call
	m.mu.Unlock()

	var out string
	var err error
	select {
	case m.sem <- struct{}{}:
		out, err = p.Translate(ctx, text)
		<-m.sem
	case <-ctx.Done():
		err = ctx.Err()
	}

	m.mu.Lock()
	delete(m.inflight, text)
	if err == nil {
		m.cache[text] = out
	}
	m.mu.Unlock()

	call.val, call.err = out, err
	close(call.done)
	return out, false, err
}
