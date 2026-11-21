package translator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"khutaf/internal/config"
)

type stubProvider struct {
	name string
	fn   func(ctx context.Context, text string) (string, error)
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Translate(ctx context.Context, text string) (string, error) {
	return s.fn(ctx, text)
}

func TestManagerRecoverPanicAndReleasesSemaphore(t *testing.T) {
	m := NewManager()
	m.mu.Lock()
	m.provider = &stubProvider{
		name: "panic-stub",
		fn: func(context.Context, string) (string, error) {
			panic("boom")
		},
	}
	m.mu.Unlock()

	_, _, err := m.Translate(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "translator panic") {
		t.Fatalf("expected translator panic error, got %v", err)
	}
	// Semaphore slot must have been released on panic.
	if len(m.sem) != 0 {
		t.Fatalf("expected semaphore to be empty after panic, len=%d", len(m.sem))
	}
	if len(m.inflight) != 0 {
		t.Fatalf("expected inflight map to be cleared after panic, len=%d", len(m.inflight))
	}
}

func TestManagerEmptyOutputReturnsErrorWithoutCaching(t *testing.T) {
	m := NewManager()
	m.mu.Lock()
	m.provider = &stubProvider{
		name: "empty-stub",
		fn: func(context.Context, string) (string, error) {
			return "   ", nil
		},
	}
	m.mu.Unlock()

	_, _, err := m.Translate(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error on empty translation output")
	}
	if m.CacheSize() != 0 {
		t.Fatalf("expected empty output not to be cached, cache size=%d", m.CacheSize())
	}
}

func TestManagerSingleflightDeduplicatesConcurrentRequests(t *testing.T) {
	var calls atomic.Int32
	m := NewManager()
	m.mu.Lock()
	m.provider = &stubProvider{
		name: "sf-stub",
		fn: func(context.Context, string) (string, error) {
			calls.Add(1)
			time.Sleep(30 * time.Millisecond)
			return "مرحبا", nil
		},
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := m.Translate(context.Background(), "Hello")
			if err != nil {
				t.Errorf("Translate error: %v", err)
			}
			if got != "مرحبا" {
				t.Errorf("got %q, want مرحبا", got)
			}
		}()
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 upstream provider call via singleflight, got %d", n)
	}
}

func TestLLMStripsThinkBlocks(t *testing.T) {
	var reply atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"role": "assistant", "content": reply.Load().(string)}},
			},
		})
	}))
	defer srv.Close()

	llm := NewLLM(config.LLMSettings{
		BaseURL:      srv.URL,
		Model:        "test-model",
		SystemPrompt: "Translate {source} to {target}",
		SourceLang:   "English",
		TargetLang:   "Arabic",
	})

	reply.Store("<think>reasoning line 1\nline 2</think>\nمرحبا")
	got, err := llm.Translate(context.Background(), "Hello")
	if err != nil || got != "مرحبا" {
		t.Fatalf("closed think strip = (%q, %v), want (%q, nil)", got, err, "مرحبا")
	}

	reply.Store("مرحبا<think>truncated reasoning without closing tag")
	got, err = llm.Translate(context.Background(), "Hello")
	if err != nil || got != "مرحبا" {
		t.Fatalf("unclosed think strip = (%q, %v), want (%q, nil)", got, err, "مرحبا")
	}

	reply.Store("<think>only thoughts</think>")
	got, err = llm.Translate(context.Background(), "OnlyThoughts")
	if err != nil || got != "" {
		t.Fatalf("think-only response = (%q, %v), want (\"\", nil)", got, err)
	}

	m := NewManager()
	m.mu.Lock()
	m.provider = llm
	m.mu.Unlock()
	if _, _, err := m.Translate(context.Background(), "OnlyThoughts"); err == nil {
		t.Fatal("expected Manager.Translate to error when stripped LLM output is empty")
	}
	if m.CacheSize() != 0 {
		t.Fatalf("expected empty post-think output not to be cached, cache size=%d", m.CacheSize())
	}
}

func TestManagerFailoverOnPrimaryErrorAndTelemetry(t *testing.T) {
	m := NewManager()
	var (
		mu     sync.Mutex
		events []string
	)
	m.SetTelemetry(func(subsys, fnName string, durUs int64, level, msg string, fields map[string]any) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, fnName+":"+level+":"+msg)
	})

	var primaryCalls atomic.Int32
	var fbCalls atomic.Int32
	m.mu.Lock()
	m.provider = &stubProvider{
		name: "primary-stub",
		fn: func(context.Context, string) (string, error) {
			primaryCalls.Add(1)
			return "", context.DeadlineExceeded
		},
	}
	m.fallback = &stubProvider{
		name: "fallback-stub",
		fn: func(context.Context, string) (string, error) {
			fbCalls.Add(1)
			return "ترجمة احتياطية", nil
		},
	}
	m.mu.Unlock()

	got, cached, err := m.Translate(context.Background(), "Hello hero")
	if err != nil || cached || got != "ترجمة احتياطية" {
		t.Fatalf("Translate failover = (%q, %v, %v), want (\"ترجمة احتياطية\", false, nil)", got, cached, err)
	}
	if primaryCalls.Load() != 1 || fbCalls.Load() != 1 {
		t.Fatalf("unexpected call counts primary=%d fallback=%d", primaryCalls.Load(), fbCalls.Load())
	}

	mu.Lock()
	joined := strings.Join(events, " | ")
	mu.Unlock()
	if !strings.Contains(joined, "Manager.Translate.Fallback:warn:primary failed; used fallback") {
		t.Fatalf("expected fallback telemetry event, got: %s", joined)
	}

	// Ensure translation was cached under fallback-stub, not primary-stub.
	if _, ok := m.Cache().Get("", "primary-stub", "Hello hero"); ok {
		t.Fatal("failed primary output should not be cached under primary provider")
	}
	if val, ok := m.Cache().Get("", "fallback-stub", "Hello hero"); !ok || val != "ترجمة احتياطية" {
		t.Fatalf("expected cache entry under fallback-stub, got (%q, %v)", val, ok)
	}
}

func TestManagerConfigureSkipsIdenticalFallback(t *testing.T) {
	m := NewManager()
	m.Configure(config.TranslatorSettings{
		Active:           "bing",
		FallbackProvider: "bing",
	})
	if m.Fallback() != "" {
		t.Fatalf("expected identical fallback to be ignored, got %q", m.Fallback())
	}

	m.Configure(config.TranslatorSettings{
		Active:           "bing",
		FallbackProvider: "google",
	})
	if m.Fallback() != "google" {
		t.Fatalf("expected fallback google, got %q", m.Fallback())
	}

	m.Configure(config.TranslatorSettings{
		Active:           "bing",
		FallbackProvider: "none",
	})
	if m.Fallback() != "" {
		t.Fatalf("expected none fallback to clear, got %q", m.Fallback())
	}
}
