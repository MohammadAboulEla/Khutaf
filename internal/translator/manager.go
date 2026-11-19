//go:build windows

package translator

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"khutaf/internal/config"
	"khutaf/internal/pipeline"
	"khutaf/internal/translator/traditional"
)

const maxConcurrentRequests = 4

// TelemetryFunc receives structured telemetry events from translators.
type TelemetryFunc func(subsys, fnName string, durUs int64, level, msg string, fields map[string]any)

type inflightCall struct {
	done    chan struct{}
	val     string
	err     error
	waiters atomic.Int64
}

// Manager holds the active provider, a persistent translation cache, and
// deduplicates concurrent identical requests.
type Manager struct {
	mu       sync.RWMutex
	hasCfg   bool
	cfg      config.TranslatorSettings
	provider Provider
	gameUID  string
	cache    *pipeline.Cache
	inflight map[string]*inflightCall
	sem      chan struct{}
	telem    atomic.Pointer[TelemetryFunc]
}

// SetTelemetry registers an optional telemetry callback.
func (m *Manager) SetTelemetry(fn TelemetryFunc) {
	if fn == nil {
		m.telem.Store(nil)
	} else {
		m.telem.Store(&fn)
	}
	m.mu.RLock()
	p := m.provider
	m.mu.RUnlock()
	if b, ok := p.(*Bing); ok {
		b.SetTelemetry(fn)
	}
}

func (m *Manager) emitTelem(fnName string, durUs int64, level, msg string, fields map[string]any) {
	if p := m.telem.Load(); p != nil && *p != nil {
		(*p)("TRANSLATOR", fnName, durUs, level, msg, fields)
	}
}

// NewManager creates a manager backed by the given cache (or an in-memory cache if nil/omitted).
func NewManager(cache ...*pipeline.Cache) *Manager {
	var c *pipeline.Cache
	if len(cache) > 0 && cache[0] != nil {
		c = cache[0]
	} else {
		c = pipeline.NewMemoryCache()
	}
	return &Manager{
		gameUID:  pipeline.NormalizeGameUID(""),
		cache:    c,
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

// SetGame sets the active game scope for cache lookups and writes.
func (m *Manager) SetGame(gamePath string) {
	m.mu.Lock()
	m.gameUID = pipeline.NormalizeGameUID(gamePath)
	m.mu.Unlock()
}

// Cache returns the underlying translation cache.
func (m *Manager) Cache() *pipeline.Cache { return m.cache }

// BuildProvider constructs a Provider from the given TranslatorSettings without mutating Manager state.
func BuildProvider(cfg config.TranslatorSettings) Provider {
	switch cfg.Active {
	case "bing":
		return NewBing(cfg.Bing)
	case "llm":
		return NewLLM(cfg.LLM)
	case "offline_llm":
		return NewOfflineLLM(cfg.OfflineLLM)
	case "none", "":
		return nil
	default:
		if tp := traditional.New(cfg.Active, cfg.Traditional); tp != nil {
			return tp
		}
		return nil
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

	p := BuildProvider(cfg)
	if b, ok := p.(*Bing); ok {
		if tp := m.telem.Load(); tp != nil && *tp != nil {
			b.SetTelemetry(*tp)
		}
	}
	m.mu.Lock()
	m.cfg = cfg
	m.hasCfg = true
	m.provider = p
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

// CacheSize returns the total number of cached translations.
func (m *Manager) CacheSize() int {
	return m.cache.Count()
}

// ClearCache removes non-manual cached translations and returns the removed count.
func (m *Manager) ClearCache() int {
	return m.cache.Clear("", false)
}

// Translate returns a cached translation or queries the provider.
// Concurrent requests for the same text share one in-flight call.
func (m *Manager) Translate(ctx context.Context, text string) (string, bool, error) {
	return m.TranslateValidated(ctx, text, nil)
}

// TranslateValidated behaves like Translate, but runs valid (if non-nil) on
// fresh provider outputs before persisting them to the cache.
func (m *Manager) TranslateValidated(ctx context.Context, text string, valid func(string) error) (string, bool, error) {
	m.mu.RLock()
	p := m.provider
	gameUID := m.gameUID
	m.mu.RUnlock()

	provName := ""
	if p != nil {
		provName = p.Name()
	}
	if cached, hit := m.cache.Get(gameUID, provName, text); hit {
		return cached, true, nil
	}
	if p == nil {
		return "", false, ErrNoProvider
	}

	infKey := gameUID + "\x00" + provName + "\x00" + text

	m.mu.Lock()
	if cached, hit := m.cache.GetSilent(gameUID, provName, text); hit {
		m.mu.Unlock()
		return cached, true, nil
	}
	if call, exists := m.inflight[infKey]; exists {
		waiters := call.waiters.Add(1)
		m.mu.Unlock()
		m.emitTelem("Manager.Translate", 0, "debug", "singleflight coalesced", map[string]any{
			"provider": provName,
			"waiters":  waiters,
		})
		select {
		case <-call.done:
			return call.val, false, call.err
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	call := &inflightCall{done: make(chan struct{})}
	m.inflight[infKey] = call
	m.mu.Unlock()

	go func() {
		reqStart := time.Now()
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()

		var out string
		var err error
		semStart := time.Now()
		select {
		case m.sem <- struct{}{}:
			semWait := time.Since(semStart)
			if semWait > 500*time.Millisecond {
				m.emitTelem("Manager.Translate", semWait.Microseconds(), "warn", "semaphore queue wait > 500ms", map[string]any{
					"provider": provName,
				})
			}
			out, err = p.Translate(reqCtx, text)
			<-m.sem
		case <-reqCtx.Done():
			err = reqCtx.Err()
		}

		if err == nil && strings.TrimSpace(out) == "" {
			m.emitTelem("Manager.Translate", time.Since(reqStart).Microseconds(), "warn", "empty translation output rejected", map[string]any{
				"provider": provName,
			})
		} else if err == nil {
			if valid != nil {
				if verr := valid(out); verr != nil {
					err = verr
					out = ""
				}
			}
			if err == nil {
				m.cache.PutAuto(gameUID, provName, text, out)
			}
		}

		m.mu.Lock()
		delete(m.inflight, infKey)
		m.mu.Unlock()

		durUs := time.Since(reqStart).Microseconds()
		if err != nil {
			m.emitTelem("Manager.Translate", durUs, "error", err.Error(), map[string]any{
				"provider": provName,
				"waiters":  call.waiters.Load(),
			})
		} else if strings.TrimSpace(out) != "" {
			m.emitTelem("Manager.Translate", durUs, "debug", "ok", map[string]any{
				"provider": provName,
				"waiters":  call.waiters.Load(),
			})
		}

		call.val, call.err = out, err
		close(call.done)
	}()

	select {
	case <-call.done:
		return call.val, false, call.err
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}
