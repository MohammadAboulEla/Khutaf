//go:build windows

package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"khutaf/internal/config"
	"khutaf/internal/translator"
)

type mockBenchProvider struct {
	name string
	fn   func(ctx context.Context, text string) (string, error)
}

func (m *mockBenchProvider) Name() string { return m.name }
func (m *mockBenchProvider) Translate(ctx context.Context, text string) (string, error) {
	return m.fn(ctx, text)
}

func TestBenchmarkTranslatorsStreamsSortsAndIsolatesState(t *testing.T) {
	store := config.NewMemoryStore()
	cfg := store.Config()
	cfg.Translator.Active = "bing"
	_ = store.SaveConfig(cfg)

	var (
		mu          sync.Mutex
		streamed    []BenchmarkItem
		doneSummary *BenchmarkSummary
	)
	doneCh := make(chan struct{})

	s := New(store, "", func(name string, data any) {
		mu.Lock()
		defer mu.Unlock()
		switch name {
		case EventBenchmarkItem:
			if item, ok := data.(BenchmarkItem); ok {
				streamed = append(streamed, item)
			}
		case EventBenchmarkDone:
			if sum, ok := data.(BenchmarkSummary); ok {
				cp := sum
				doneSummary = &cp
				close(doneCh)
			}
		}
	})
	defer s.Close()

	s.providerBuilder = func(tc config.TranslatorSettings) translator.Provider {
		switch tc.Active {
		case "bing":
			return &mockBenchProvider{
				name: "bing",
				fn: func(ctx context.Context, text string) (string, error) {
					time.Sleep(45 * time.Millisecond)
					return "البوابة القديمة مغلقة.", nil
				},
			}
		case "google":
			return &mockBenchProvider{
				name: "google",
				fn: func(ctx context.Context, text string) (string, error) {
					time.Sleep(10 * time.Millisecond)
					return "العثور على <color=#FFD700>المفتاح الفضي</color> {player} [E]", nil
				},
			}
		case "yandex":
			return &mockBenchProvider{
				name: "yandex",
				fn: func(ctx context.Context, text string) (string, error) {
					return "", errors.New("HTTP 429 rate limited")
				},
			}
		case "modernmt":
			return &mockBenchProvider{
				name: "modernmt",
				fn: func(ctx context.Context, text string) (string, error) {
					<-ctx.Done()
					return "", ctx.Err()
				},
			}
		default:
			return nil
		}
	}

	req := BenchmarkRequest{
		RunID:     "run-1",
		Mode:      "single",
		Text:      "Found <color=#FFD700>Silver Key</color> {player} [E]",
		Providers: []string{"bing", "google", "yandex", "modernmt", "llm"},
		TimeoutMs: 120, // clamped to 2000ms min in production, test fast cancel/timeout via context? Let's test explicit timeout by cancelling modernmt with short deadline or testing ctx timeout.
		Config:    cfg.Translator,
	}
	// Note: TimeoutMs clamps at 2000ms minimum, so modernmt would wait 2s unless we pass a provider that returns DeadlineExceeded directly.
	// Replace modernmt with immediate DeadlineExceeded simulation to keep unit test < 100ms.
	s.providerBuilder = func(tc config.TranslatorSettings) translator.Provider {
		switch tc.Active {
		case "bing":
			return &mockBenchProvider{
				name: "bing",
				fn: func(ctx context.Context, text string) (string, error) {
					time.Sleep(45 * time.Millisecond)
					return "تم العثور على المفتاح الفضي", nil // drops tags & placeholders
				},
			}
		case "google":
			return &mockBenchProvider{
				name: "google",
				fn: func(ctx context.Context, text string) (string, error) {
					time.Sleep(10 * time.Millisecond)
					return "وجدت <color=#FFD700>المفتاح الفضي</color> {player} [E]", nil
				},
			}
		case "yandex":
			return &mockBenchProvider{
				name: "yandex",
				fn: func(ctx context.Context, text string) (string, error) {
					return "", errors.New("HTTP 429 rate limited")
				},
			}
		case "modernmt":
			return &mockBenchProvider{
				name: "modernmt",
				fn: func(ctx context.Context, text string) (string, error) {
					return "", context.DeadlineExceeded
				},
			}
		default:
			return nil
		}
	}

	summary, err := s.BenchmarkTranslators(context.Background(), req)
	if err != nil {
		t.Fatalf("BenchmarkTranslators: %v", err)
	}
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventBenchmarkDone")
	}

	if summary.Total != 5 || summary.Succeeded != 2 || summary.Failed != 2 {
		t.Fatalf("unexpected summary counts: %+v", summary)
	}
	if len(summary.Items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(summary.Items))
	}
	// Fastest OK provider ("google") must sort first.
	if summary.Items[0].ProviderID != "google" || summary.Items[0].Status != "ok" {
		t.Fatalf("expected google first, got %+v", summary.Items[0])
	}
	if summary.Items[0].QualityScore != 100 || !summary.Items[0].TagsPreserved || !summary.Items[0].PlaceholdersPreserved || !summary.Items[0].HasArabic {
		t.Fatalf("expected perfect game safety score for google, got %+v", summary.Items[0])
	}
	if summary.Items[1].ProviderID != "bing" || summary.Items[1].TagsPreserved || summary.Items[1].PlaceholdersPreserved {
		t.Fatalf("expected bing second with dropped tags/placeholders, got %+v", summary.Items[1])
	}
	// Verify timeout, error, and skipped statuses.
	statusByID := map[string]string{}
	for _, it := range summary.Items {
		statusByID[it.ProviderID] = it.Status
	}
	if statusByID["modernmt"] != "timeout" || statusByID["yandex"] != "error" || statusByID["llm"] != "skipped" {
		t.Fatalf("unexpected status map: %+v", statusByID)
	}

	mu.Lock()
	streamCount := len(streamed)
	hasDone := doneSummary != nil && doneSummary.RunID == "run-1"
	mu.Unlock()
	if streamCount != 5 || !hasDone {
		t.Fatalf("expected 5 streamed items and done summary, got stream=%d hasDone=%v", streamCount, hasDone)
	}

	// Ensure active translator and cache were untouched.
	if got := s.translator.Active(); got != "bing" {
		t.Fatalf("active provider mutated: got %q, want bing", got)
	}
	if cnt := s.CacheCount(); cnt != 0 {
		t.Fatalf("benchmark polluted cache: count=%d", cnt)
	}
}

func TestBenchmarkTranslatorsCancellationAndPackScoring(t *testing.T) {
	s := New(config.NewMemoryStore(), "", nil)
	defer s.Close()

	started := make(chan struct{})
	s.providerBuilder = func(tc config.TranslatorSettings) translator.Provider {
		return &mockBenchProvider{
			name: tc.Active,
			fn: func(ctx context.Context, text string) (string, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				return "", ctx.Err()
			},
		}
	}

	go func() {
		<-started
		s.CancelBenchmark()
	}()

	sum, err := s.BenchmarkTranslators(context.Background(), BenchmarkRequest{
		Mode:      "pack",
		Providers: []string{"bing"},
	})
	if err != nil {
		t.Fatalf("BenchmarkTranslators: %v", err)
	}
	if !sum.Cancelled || len(sum.Items) != 1 || sum.Items[0].Status != "cancelled" || sum.Items[0].TotalSamples != 4 {
		t.Fatalf("expected cancelled 4-sample summary, got %+v", sum)
	}
}
