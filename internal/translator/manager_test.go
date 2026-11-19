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
