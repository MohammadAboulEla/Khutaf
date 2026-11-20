//go:build windows

package session

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/pipeline"
	"khutaf/internal/translator"
)

// DefaultSingleBenchmarkText is the default line used when benchmarking a single sample.
const DefaultSingleBenchmarkText = "The ancient gate is sealed. Find the Silver Key to proceed!"

// GameBenchmarkPack returns 4 realistic Unity strings covering dialogue, rich tags, placeholders, and UI text.
func GameBenchmarkPack() []string {
	return []string{
		"The ancient gate is sealed. Find the Silver Key to proceed!",
		"Found <color=#FFD700>Silver Key</color> in the chest.",
		"Welcome back, {player}! Press [E] to talk.",
		"Save Game & Continue",
	}
}

// BenchmarkRequest configures a parallel multi-provider translation benchmark.
type BenchmarkRequest struct {
	RunID              string                    `json:"runId"`
	Mode               string                    `json:"mode"` // "single" | "pack"
	Text               string                    `json:"text"`
	Providers          []string                  `json:"providers"`
	IncludeTraditional bool                      `json:"includeTraditional"`
	IncludeLLM         bool                      `json:"includeLlm"`
	IncludeOfflineLLM  bool                      `json:"includeOfflineLlm"`
	TimeoutMs          int                       `json:"timeoutMs"`
	Config             config.TranslatorSettings `json:"config"`
}

// BenchmarkSampleResult holds the benchmark result for one test sentence on one provider.
type BenchmarkSampleResult struct {
	Source                string `json:"source"`
	Translated            string `json:"translated"`
	Shaped                string `json:"shaped"`
	ElapsedMs             int64  `json:"elapsedMs"`
	Error                 string `json:"error,omitempty"`
	TagsPreserved         bool   `json:"tagsPreserved"`
	PlaceholdersPreserved bool   `json:"placeholdersPreserved"`
	HasArabic             bool   `json:"hasArabic"`
}

// BenchmarkItem holds aggregated speed and quality results for a single provider.
type BenchmarkItem struct {
	RunID                 string                  `json:"runId"`
	ProviderID            string                  `json:"providerId"`
	Label                 string                  `json:"label"`
	Category              string                  `json:"category"` // "traditional" | "llm" | "offline_llm"
	Status                string                  `json:"status"`   // "ok" | "partial" | "error" | "timeout" | "skipped" | "cancelled"
	Error                 string                  `json:"error,omitempty"`
	AvgMs                 int64                   `json:"avgMs"`
	TotalMs               int64                   `json:"totalMs"`
	SpeedTier             string                  `json:"speedTier"` // "fast" | "medium" | "slow" | ""
	SampleOutput          string                  `json:"sampleOutput"`
	SampleShaped          string                  `json:"sampleShaped"`
	TagsPreserved         bool                    `json:"tagsPreserved"`
	PlaceholdersPreserved bool                    `json:"placeholdersPreserved"`
	HasArabic             bool                    `json:"hasArabic"`
	QualityScore          int                     `json:"qualityScore"`
	Succeeded             int                     `json:"succeeded"`
	TotalSamples          int                     `json:"totalSamples"`
	Samples               []BenchmarkSampleResult `json:"samples"`
}

// BenchmarkSummary is returned and emitted when all provider benchmarks finish.
type BenchmarkSummary struct {
	RunID     string          `json:"runId"`
	Total     int             `json:"total"`
	Succeeded int             `json:"succeeded"`
	Failed    int             `json:"failed"`
	Cancelled bool            `json:"cancelled"`
	ElapsedMs int64           `json:"elapsedMs"`
	Items     []BenchmarkItem `json:"items"`
}

type benchmarkProviderMeta struct {
	ID       string
	Label    string
	Category string
}

var allBenchmarkProviders = []benchmarkProviderMeta{
	{ID: "bing", Label: "Bing", Category: "traditional"},
	{ID: "google", Label: "Google", Category: "traditional"},
	{ID: "microsoft", Label: "Microsoft", Category: "traditional"},
	{ID: "yandex", Label: "Yandex", Category: "traditional"},
	{ID: "modernmt", Label: "ModernMT", Category: "traditional"},
	{ID: "alibaba", Label: "Alibaba", Category: "traditional"},
	{ID: "youdao", Label: "Youdao", Category: "traditional"},
	{ID: "papago", Label: "Papago", Category: "traditional"},
	{ID: "qqtransmart", Label: "qqTranSmart", Category: "traditional"},
	{ID: "qqimt", Label: "qqimt", Category: "traditional"},
	{ID: "translatecom", Label: "Translate.com", Category: "traditional"},
	{ID: "volcengine", Label: "Volcengine", Category: "traditional"},
	{ID: "lingocloud", Label: "LingoCloud", Category: "traditional"},
	{ID: "llm", Label: "Cloud / OpenAI LLM", Category: "llm"},
	{ID: "offline_llm", Label: "Local Offline LLM", Category: "offline_llm"},
}

var richTagRe = regexp.MustCompile(`(?i)</?[a-z][^>]*>`)

func richTagsPreserved(source, trans string) bool {
	srcTags := richTagRe.FindAllString(source, -1)
	dstTags := richTagRe.FindAllString(trans, -1)
	if len(srcTags) != len(dstTags) {
		return false
	}
	counts := make(map[string]int, len(srcTags))
	for _, t := range srcTags {
		counts[strings.ToLower(strings.TrimSpace(t))]++
	}
	for _, t := range dstTags {
		k := strings.ToLower(strings.TrimSpace(t))
		counts[k]--
		if counts[k] < 0 {
			return false
		}
	}
	return true
}

func resolveBenchmarkTargets(req BenchmarkRequest) []benchmarkProviderMeta {
	byID := make(map[string]benchmarkProviderMeta, len(allBenchmarkProviders))
	for _, m := range allBenchmarkProviders {
		byID[m.ID] = m
	}
	if len(req.Providers) > 0 {
		seen := make(map[string]bool, len(req.Providers))
		out := make([]benchmarkProviderMeta, 0, len(req.Providers))
		for _, raw := range req.Providers {
			id := strings.ToLower(strings.TrimSpace(raw))
			if seen[id] {
				continue
			}
			if m, ok := byID[id]; ok {
				seen[id] = true
				out = append(out, m)
			}
		}
		return out
	}
	includeTrad := req.IncludeTraditional
	if !req.IncludeTraditional && !req.IncludeLLM && !req.IncludeOfflineLLM {
		includeTrad = true
	}
	var out []benchmarkProviderMeta
	for _, m := range allBenchmarkProviders {
		switch m.Category {
		case "traditional":
			if includeTrad {
				out = append(out, m)
			}
		case "llm":
			if req.IncludeLLM {
				out = append(out, m)
			}
		case "offline_llm":
			if req.IncludeOfflineLLM {
				out = append(out, m)
			}
		}
	}
	return out
}

func resolveBenchmarkSamples(req BenchmarkRequest) []string {
	if strings.EqualFold(strings.TrimSpace(req.Mode), "pack") {
		return GameBenchmarkPack()
	}
	t := strings.TrimSpace(req.Text)
	if t == "" {
		t = DefaultSingleBenchmarkText
	}
	return []string{t}
}

func isLocalhostURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

func expectArabicTarget(category, id string, cfg config.TranslatorSettings) bool {
	var target string
	switch category {
	case "llm":
		target = cfg.LLM.TargetLang
	case "offline_llm":
		target = cfg.OfflineLLM.TargetLang
	default:
		if id == "bing" {
			target = cfg.Bing.TargetLang
		} else {
			target = cfg.Traditional.TargetLang
		}
	}
	t := strings.ToLower(strings.TrimSpace(target))
	return t == "" || strings.HasPrefix(t, "ar") || strings.Contains(t, "arab")
}

func validateBenchmarkProvider(meta benchmarkProviderMeta, cfg config.TranslatorSettings) string {
	switch meta.ID {
	case "llm":
		if strings.TrimSpace(cfg.LLM.BaseURL) == "" || strings.TrimSpace(cfg.LLM.Model) == "" {
			return "Cloud LLM requires Base URL and Model"
		}
		if strings.TrimSpace(cfg.LLM.APIKey) == "" && !isLocalhostURL(cfg.LLM.BaseURL) {
			return "Cloud LLM API Key is missing"
		}
	case "offline_llm":
		if strings.TrimSpace(cfg.OfflineLLM.BaseURL) == "" || strings.TrimSpace(cfg.OfflineLLM.Model) == "" {
			return "Offline LLM requires Server URL and Model"
		}
	}
	return ""
}

func speedTierForMs(ms int64) string {
	if ms < 500 {
		return "fast"
	}
	if ms <= 1500 {
		return "medium"
	}
	return "slow"
}

func scoreBenchmarkSample(src, trans string, expectArabic bool) (tagsOK, phOK, hasAr bool, score int) {
	trimmed := strings.TrimSpace(trans)
	tagsOK = richTagsPreserved(src, trans)
	phOK = pipeline.PlaceholdersPreserved(src, trans)
	hasAr = arabic.HasArabic(trans)
	if trimmed == "" {
		return false, false, false, 0
	}
	if !strings.EqualFold(trimmed, strings.TrimSpace(src)) {
		score += 40
	} else {
		score += 10
	}
	if !expectArabic || hasAr {
		score += 20
	}
	if tagsOK {
		score += 20
	}
	if phOK {
		score += 20
	}
	return tagsOK, phOK, hasAr, score
}

// CancelBenchmark cancels any running translator benchmark.
func (s *Session) CancelBenchmark() {
	s.benchMu.Lock()
	cancel := s.benchCancel
	s.benchCancel = nil
	s.benchMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// BenchmarkTranslators tests selected translation providers concurrently without altering saved config or cache.
func (s *Session) BenchmarkTranslators(ctx context.Context, req BenchmarkRequest) (BenchmarkSummary, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	runID := strings.TrimSpace(req.RunID)
	if runID == "" {
		runID = time.Now().Format("20060102150405.000000000")
	}
	timeoutMs := req.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 10000
	} else if timeoutMs < 2000 {
		timeoutMs = 2000
	} else if timeoutMs > 30000 {
		timeoutMs = 30000
	}
	perProviderTimeout := time.Duration(timeoutMs) * time.Millisecond

	targets := resolveBenchmarkTargets(req)
	samples := resolveBenchmarkSamples(req)

	baseCfg := req.Config
	if baseCfg.Bing.TargetLang == "" && baseCfg.Traditional.TargetLang == "" && baseCfg.LLM.BaseURL == "" {
		baseCfg = s.store.Config().Translator
	}

	s.benchMu.Lock()
	if s.benchCancel != nil {
		s.benchCancel()
	}
	benchCtx, cancel := context.WithCancel(ctx)
	s.benchCancel = cancel
	s.benchMu.Unlock()

	defer func() {
		wasCancelled := benchCtx.Err() != nil
		cancel()
		if !wasCancelled {
			s.benchMu.Lock()
			s.benchCancel = nil
			s.benchMu.Unlock()
		}
	}()

	builder := s.providerBuilder
	if builder == nil {
		builder = translator.BuildProvider
	}

	start := time.Now()
	items := make([]BenchmarkItem, len(targets))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup

	for i, meta := range targets {
		wg.Add(1)
		go func(idx int, m benchmarkProviderMeta) {
			defer wg.Done()
			item := BenchmarkItem{
				RunID:        runID,
				ProviderID:   m.ID,
				Label:        m.Label,
				Category:     m.Category,
				TotalSamples: len(samples),
				Samples:      make([]BenchmarkSampleResult, 0, len(samples)),
			}

			provCfg := baseCfg
			provCfg.Active = m.ID
			if reason := validateBenchmarkProvider(m, provCfg); reason != "" {
				item.Status = "skipped"
				item.Error = reason
				items[idx] = item
				s.post(EventBenchmarkItem, item)
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-benchCtx.Done():
				item.Status = "cancelled"
				item.Error = "benchmark cancelled"
				items[idx] = item
				s.post(EventBenchmarkItem, item)
				return
			}

			p := builder(provCfg)
			if p == nil {
				item.Status = "skipped"
				item.Error = "provider unavailable"
				items[idx] = item
				s.post(EventBenchmarkItem, item)
				return
			}

			provCtx, provCancel := context.WithTimeout(benchCtx, perProviderTimeout)
			defer provCancel()

			expectAr := expectArabicTarget(m.Category, m.ID, provCfg)
			provStart := time.Now()
			var okMsSum int64
			var scoreSum int
			var lastErr error
			allTagsOK := true
			allPhOK := true
			anyArabic := false

			for _, src := range samples {
				if benchCtx.Err() != nil {
					lastErr = benchCtx.Err()
					break
				}
				sStart := time.Now()
				out, err := p.Translate(provCtx, src)
				sMs := time.Since(sStart).Milliseconds()
				sr := BenchmarkSampleResult{
					Source:    src,
					ElapsedMs: sMs,
				}
				if err == nil && strings.TrimSpace(out) == "" {
					err = errors.New("provider returned empty translation")
				}
				if err != nil {
					sr.Error = err.Error()
					lastErr = err
				} else {
					tagsOK, phOK, hasAr, sc := scoreBenchmarkSample(src, out, expectAr)
					sr.Translated = out
					sr.Shaped = s.ShapeForEmbed(out)
					sr.TagsPreserved = tagsOK
					sr.PlaceholdersPreserved = phOK
					sr.HasArabic = hasAr
					item.Succeeded++
					okMsSum += sMs
					scoreSum += sc
					if !tagsOK {
						allTagsOK = false
					}
					if !phOK {
						allPhOK = false
					}
					if hasAr {
						anyArabic = true
					}
					if item.SampleOutput == "" {
						item.SampleOutput = out
						item.SampleShaped = sr.Shaped
					}
				}
				item.Samples = append(item.Samples, sr)
			}

			item.TotalMs = time.Since(provStart).Milliseconds()
			if item.Succeeded > 0 {
				item.AvgMs = okMsSum / int64(item.Succeeded)
				if item.AvgMs <= 0 && item.TotalMs > 0 {
					item.AvgMs = item.TotalMs / int64(item.Succeeded)
				}
				item.SpeedTier = speedTierForMs(item.AvgMs)
				item.TagsPreserved = allTagsOK
				item.PlaceholdersPreserved = allPhOK
				item.HasArabic = anyArabic
				item.QualityScore = scoreSum / len(samples)
			}

			switch {
			case item.Succeeded == len(samples) && len(samples) > 0:
				item.Status = "ok"
			case item.Succeeded > 0:
				item.Status = "partial"
				if lastErr != nil {
					item.Error = lastErr.Error()
				}
			case benchCtx.Err() != nil:
				item.Status = "cancelled"
				item.Error = "benchmark cancelled"
			case errors.Is(lastErr, context.DeadlineExceeded) || errors.Is(provCtx.Err(), context.DeadlineExceeded):
				item.Status = "timeout"
				item.Error = "timed out"
			default:
				item.Status = "error"
				if lastErr != nil {
					item.Error = lastErr.Error()
				} else {
					item.Error = "translation failed"
				}
			}

			items[idx] = item
			s.post(EventBenchmarkItem, item)
		}(i, meta)
	}

	wg.Wait()

	sortBenchmarkItems(items)
	summary := BenchmarkSummary{
		RunID:     runID,
		Total:     len(items),
		Cancelled: benchCtx.Err() != nil,
		ElapsedMs: time.Since(start).Milliseconds(),
		Items:     items,
	}
	for _, it := range items {
		if it.Status == "ok" || it.Status == "partial" {
			summary.Succeeded++
		} else if it.Status != "skipped" {
			summary.Failed++
		}
	}
	s.post(EventBenchmarkDone, summary)
	return summary, nil
}

func benchmarkStatusRank(status string) int {
	switch status {
	case "ok":
		return 0
	case "partial":
		return 1
	case "timeout":
		return 2
	case "error":
		return 3
	case "skipped":
		return 4
	case "cancelled":
		return 5
	default:
		return 6
	}
}

func sortBenchmarkItems(items []BenchmarkItem) {
	sort.Slice(items, func(i, j int) bool {
		ri, rj := benchmarkStatusRank(items[i].Status), benchmarkStatusRank(items[j].Status)
		if ri != rj {
			return ri < rj
		}
		if ri <= 1 {
			if items[i].AvgMs != items[j].AvgMs {
				return items[i].AvgMs < items[j].AvgMs
			}
			if items[i].QualityScore != items[j].QualityScore {
				return items[i].QualityScore > items[j].QualityScore
			}
		}
		return items[i].Label < items[j].Label
	})
}
