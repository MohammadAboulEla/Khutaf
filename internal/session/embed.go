//go:build windows

package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
	"khutaf/internal/translator"
)

// SetEmbed toggles embedded translation for a hook and persists the choice.
func (s *Session) SetEmbed(key string, enabled bool) error {
	if s.host == nil {
		return errors.New("hook host unavailable")
	}
	s.mu.Lock()
	h, ok := s.hooks[key]
	path := s.gamePath
	sample := ""
	if ok {
		if enabled {
			s.embedding[key] = true
			sample = truncateSample(s.lastOutput[key], 120)
		} else {
			delete(s.embedding, key)
		}
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown hook %s", key)
	}
	s.host.UseEmbed(h.TP, enabled)
	if err := s.store.UpdateGame(path, func(g *config.GameProfile) {
		kept := g.EmbedHooks[:0]
		for _, e := range g.EmbedHooks {
			if !(e.HookCode == h.HookCode && e.Ctx == h.Ctx && e.Ctx2 == h.Ctx2) {
				kept = append(kept, e)
			}
		}
		g.EmbedHooks = kept
		if enabled {
			g.EmbedHooks = append(g.EmbedHooks, config.EmbedHook{
				HookCode: h.HookCode,
				HookName: h.HookName,
				Ctx:      h.Ctx,
				Ctx2:     h.Ctx2,
				Sample:   sample,
			})
		}
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}); err != nil {
		return err
	}
	s.post(EventHookNew, HookView{HookInfo: h, Embedding: enabled})
	return nil
}

func truncateSample(raw string, maxRunes int) string {
	clean := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(raw))
	if clean == "" || maxRunes <= 0 {
		return ""
	}
	r := []rune(clean)
	if len(r) <= maxRunes {
		return clean
	}
	return strings.TrimSpace(string(r[:maxRunes-1])) + "…"
}

func (s *Session) pushEmbedSettings(pid uint32) {
	if s.host == nil {
		return
	}
	cfg := *s.embedCfg.Load()
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	font := ""
	unityDir := ""
	if cfg.ChangeFont {
		font = cfg.FontFamily
		if cfg.UseUnityBundle && strings.HasPrefix(strings.ToLower(engine), "unity") {
			unityDir = s.unityFontDir()
		}
	}
	waitMs := cfg.TimeoutMs
	if waitMs < 100 {
		waitMs = 100
	} else if waitMs > 30000 {
		waitMs = 30000
	}
	pending := strings.TrimSpace(cfg.PendingText)
	if pending != "" && arabic.HasArabic(pending) {
		pending = s.ShapeForEmbed(pending)
	}
	s.host.SettingsEx(pid, hook.EmbedSettings{
		WaitTimeMs:          uint32(waitMs),
		FontFamily:          font,
		DisplayMode:         hook.DisplayMode(cfg.DisplayMode),
		ClearText:           cfg.ClearText,
		ChangeFontSize:      cfg.ChangeFontSize,
		FontSizeRelative:    cfg.FontSizeRelative,
		UnityFontDir:        unityDir,
		StripTypewriterTags: cfg.StripTypewriterTags,
		NonBlocking:         cfg.NonBlocking,
		PendingText:         pending,
		VerboseHooks:        cfg.VerboseHooks,
	})
}

func (s *Session) unityFontDir() string {
	dir, count := s.unityFontMatches()
	if count > 0 {
		return dir
	}
	return ""
}

// ---- translation ----

// Translate runs the full pipeline (pre-rules, glossary, translator, post-rules,
// placeholder check) and returns the logical (unshaped) translation.
func (s *Session) Translate(ctx context.Context, text string) (string, bool, error) {
	return s.translateWith(ctx, text, s.translator.Active(), nil)
}

// TranslateWithConfig runs the full pipeline using cfg without mutating saved settings or cache.
func (s *Session) TranslateWithConfig(ctx context.Context, cfg config.TranslatorSettings, text string) (string, error) {
	p := translator.BuildProvider(cfg)
	if p == nil {
		return "", errors.New("no translator provider selected")
	}
	trans, _, err := s.translateWith(ctx, text, p.Name(), func(c context.Context, in string) (string, bool, error) {
		out, err := p.Translate(c, in)
		return out, false, err
	})
	return trans, err
}

func isLLMProvider(name string) bool {
	return name == "llm" || name == "offline_llm" ||
		strings.HasPrefix(name, "llm:") || strings.HasPrefix(name, "offline_llm:")
}

func (s *Session) translateWith(ctx context.Context, text, providerName string, fn func(context.Context, string) (string, bool, error)) (string, bool, error) {
	r := s.rules.Load()
	preText := r.pre.Apply(text)
	if isLLMProvider(providerName) {
		if gp := r.glossary.PromptContext(preText); gp != "" {
			ctx = translator.WithGlossaryPrompt(ctx, gp)
		}
	} else {
		preText = r.glossary.Apply(preText)
	}
	var (
		trans  string
		cached bool
		err    error
	)
	if fn != nil {
		trans, cached, err = fn(ctx, preText)
	} else {
		trans, cached, err = s.translator.TranslateValidated(ctx, preText, func(raw string) error {
			if !pipeline.PlaceholdersPreserved(preText, r.post.Apply(raw)) {
				return errors.New("translation dropped [..]/{..} placeholders")
			}
			return nil
		})
	}
	if err != nil {
		return "", false, err
	}
	trans = r.post.Apply(trans)
	if !pipeline.PlaceholdersPreserved(preText, trans) {
		return "", cached, errors.New("translation dropped [..]/{..} placeholders")
	}
	return trans, cached, nil
}

// ShapeForEmbed applies wrapping, shaping and visual ordering with current settings.
func (s *Session) ShapeForEmbed(trans string) string {
	var start time.Time
	debugging := s.debugMode.Load()
	if debugging {
		start = time.Now()
	}
	cfg := s.embedCfg.Load()
	out := arabic.Process(trans, arabic.Options{
		MaxLineLength: cfg.MaxLineLength,
		ForceWrap:     cfg.LimitLineLength,
		DisplayMode:   arabic.DisplayMode(cfg.DisplayMode),
	})
	if debugging {
		s.recordTelemetry(SubArabicShaper, "arabic.Process", "trace", "shaped", time.Since(start).Microseconds(), false, map[string]any{
			"inRunes":  len([]rune(trans)),
			"outRunes": len([]rune(out)),
		})
	}
	return out
}

func (s *Session) handleEmbed(text string, tp hook.ThreadParam) {
	if strings.TrimSpace(text) != "" {
		s.setLastEmbed(text, tp)
	}
	if s.paused.Load() {
		if s.host != nil {
			s.host.EmbedCallback(tp, text, "")
		}
		return
	}
	start := time.Now()
	line := EmbedLine{Key: tp.Key(), Original: text}
	debugging := s.debugMode.Load()

	var (
		trans      string
		shaped     string
		cached     bool
		err        error
		preUs      int64
		transUs    int64
		postUs     int64
		shapeUs    int64
		callbackUs int64
		seq        uint64
	)

	if debugging {
		seq = s.debugSeq.Add(1)
	}

	r := s.rules.Load()
	t0 := time.Now()
	preText := r.pre.Apply(text)
	provName := s.translator.Active()
	var glossaryPrompt string
	if isLLMProvider(provName) {
		glossaryPrompt = r.glossary.PromptContext(preText)
	} else {
		preText = r.glossary.Apply(preText)
	}
	preUs = time.Since(t0).Microseconds()

	ecfg := s.embedCfg.Load()
	t1 := time.Now()
	if debugging && !s.debugRealTrans.Load() {
		trans = fmt.Sprintf("نص اختباري %d", seq)
	} else {
		timeout := time.Duration(ecfg.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		if ecfg.NonBlocking {
			if timeout < 12*time.Second {
				timeout = 12 * time.Second
			}
		} else {
			// Leave headroom so the reply arrives before the game stops waiting.
			timeout = timeout * 9 / 10
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		if glossaryPrompt != "" {
			ctx = translator.WithGlossaryPrompt(ctx, glossaryPrompt)
		}
		trans, cached, err = s.translator.TranslateValidated(ctx, preText, func(raw string) error {
			if !pipeline.PlaceholdersPreserved(preText, r.post.Apply(raw)) {
				return errors.New("translation dropped [..]/{..} placeholders")
			}
			return nil
		})
		cancel()
	}
	transUs = time.Since(t1).Microseconds()

	if err != nil {
		line.Error = err.Error()
	} else {
		t2 := time.Now()
		trans = r.post.Apply(trans)
		if (!debugging || s.debugRealTrans.Load()) && !pipeline.PlaceholdersPreserved(preText, trans) {
			err = errors.New("translation dropped [..]/{..} placeholders")
			line.Error = err.Error()
		}
		postUs = time.Since(t2).Microseconds()

		if err == nil {
			t3 := time.Now()
			shaped = s.ShapeForEmbed(trans)
			shapeUs = time.Since(t3).Microseconds()
		}
	}

	// Always answer so the game thread is released immediately.
	tCb := time.Now()
	s.host.EmbedCallback(tp, text, shaped)
	callbackUs = time.Since(tCb).Microseconds()

	totalDur := time.Since(start)
	line.Translated, line.Shaped, line.Cached = trans, shaped, cached
	line.ElapsedMs = totalDur.Milliseconds()
	s.post(EventEmbedLine, line)

	timeoutMs := int64(ecfg.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}
	lvl := "trace"
	msg := "ok"
	isTimeout := false
	if err != nil {
		msg = err.Error()
		if strings.Contains(msg, "placeholders") {
			lvl = "warn"
		} else {
			lvl = "error"
		}
		isTimeout = isTimeoutMsg(msg)
	} else if !ecfg.NonBlocking && totalDur.Milliseconds() > (timeoutMs*80)/100 {
		lvl = "warn"
		msg = "embed latency > 80% of TimeoutMs"
	}
	if debugging || lvl == "warn" || lvl == "error" {
		glossaryMode := "Apply"
		if isLLMProvider(provName) {
			glossaryMode = "PromptContext"
		}
		s.recordTelemetry(SubPipelineCache, "Session.handleEmbed", lvl, msg, totalDur.Microseconds(), isTimeout, map[string]any{
			"key":          line.Key,
			"cached":       cached,
			"glossaryMode": glossaryMode,
			"preRulesUs":   preUs,
			"translateUs":  transUs,
			"postRulesUs":  postUs,
			"shapeUs":      shapeUs,
			"callbackUs":   callbackUs,
			"totalUs":      totalDur.Microseconds(),
			"origLen":      len([]rune(text)),
		})
	}

	if debugging {
		s.post(EventDebugTrace, DebugTrace{
			Seq:         seq,
			Time:        start.Format("15:04:05.000"),
			Key:         line.Key,
			OrigLen:     len([]rune(text)),
			Cached:      cached,
			PreRulesUs:  preUs,
			TranslateUs: transUs,
			PostRulesUs: postUs,
			ShapeUs:     shapeUs,
			CallbackUs:  callbackUs,
			TotalUs:     totalDur.Microseconds(),
			Error:       line.Error,
		})
	}
}
