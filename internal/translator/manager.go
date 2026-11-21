package translator

import (
	"context"
	"errors"
	"fmt"
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
	fallback Provider
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
	fb := m.fallback
	m.mu.RUnlock()
	if b, ok := p.(*Bing); ok {
		b.SetTelemetry(fn)
	}
	if b, ok := fb.(*Bing); ok {
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
	var fb Provider
	fbID := strings.TrimSpace(cfg.FallbackProvider)
	if fbID != "" && fbID != "none" && fbID != cfg.Active {
		fbCfg := cfg
		fbCfg.Active = fbID
		fb = BuildProvider(fbCfg)
	}
	if tp := m.telem.Load(); tp != nil && *tp != nil {
		if b, ok := p.(*Bing); ok {
			b.SetTelemetry(*tp)
		}
		if b, ok := fb.(*Bing); ok {
			b.SetTelemetry(*tp)
		}
	}
	m.mu.Lock()
	m.cfg = cfg
	m.hasCfg = true
	m.provider = p
	m.fallback = fb
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

// Fallback returns the configured fallback provider name, or "" when disabled.
func (m *Manager) Fallback() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.fallback == nil {
		return ""
	}
	return m.fallback.Name()
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
	fb := m.fallback
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
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("translator panic: %v", r)
				out = ""
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
			} else {
				m.emitTelem("Manager.Translate", durUs, "debug", "ok", map[string]any{
					"provider": provName,
					"waiters":  call.waiters.Load(),
				})
			}

			call.val, call.err = out, err
			close(call.done)
		}()

		semStart := time.Now()
		select {
		case m.sem <- struct{}{}:
			semWait := time.Since(semStart)
			if semWait > 500*time.Millisecond {
				m.emitTelem("Manager.Translate", semWait.Microseconds(), "warn", "semaphore queue wait > 500ms", map[string]any{
					"provider": provName,
				})
			}
			func() {
				defer func() { <-m.sem }()
				out, err = p.Translate(reqCtx, text)
			}()
		case <-reqCtx.Done():
			err = reqCtx.Err()
		}

		if err == nil && strings.TrimSpace(out) == "" {
			err = errors.New("provider returned empty translation")
			out = ""
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

		if err != nil && fb != nil && ctx.Err() == nil {
			primaryErr := err
			fbName := fb.Name()
			if fbCached, hit := m.cache.GetSilent(gameUID, fbName, text); hit {
				m.emitTelem("Manager.Translate.Fallback", 0, "warn", "primary failed; used fallback cache", map[string]any{
					"primary":    provName,
					"fallback":   fbName,
					"primaryErr": primaryErr.Error(),
				})
				out, err = fbCached, nil
				return
			}

			fbStart := time.Now()
			fbCtx, fbCancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer fbCancel()

			var fbOut string
			var fbErr error
			select {
			case m.sem <- struct{}{}:
				func() {
					defer func() { <-m.sem }()
					fbOut, fbErr = fb.Translate(fbCtx, text)
				}()
			case <-fbCtx.Done():
				fbErr = fbCtx.Err()
			}
			if fbErr == nil && strings.TrimSpace(fbOut) == "" {
				fbErr = errors.New("fallback provider returned empty translation")
			}
			if fbErr == nil && valid != nil {
				if verr := valid(fbOut); verr != nil {
					fbErr = verr
				}
			}

			fbDurUs := time.Since(fbStart).Microseconds()
			if fbErr == nil {
				m.cache.PutAuto(gameUID, fbName, text, fbOut)
				m.emitTelem("Manager.Translate.Fallback", fbDurUs, "warn", "primary failed; used fallback", map[string]any{
					"primary":    provName,
					"fallback":   fbName,
					"primaryErr": primaryErr.Error(),
				})
				out, err = fbOut, nil
			} else {
				m.emitTelem("Manager.Translate.Fallback", fbDurUs, "error", fbErr.Error(), map[string]any{
					"primary":    provName,
					"fallback":   fbName,
					"primaryErr": primaryErr.Error(),
				})
				err = fmt.Errorf("%w (fallback %s failed: %v)", primaryErr, fbName, fbErr)
				out = ""
			}
		}
	}()

	select {
	case <-call.done:
		return call.val, false, call.err
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}
