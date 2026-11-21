//go:build windows

package session

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
	"khutaf/internal/translator"
)

const (
	autoAttachInterval       = 3 * time.Second
	autoAttachDetachCooldown = 15 * time.Second
)

func (s *Session) startAutoAttachWatcher() {
	ticker := time.NewTicker(autoAttachInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.eventStop:
			return
		case <-ticker.C:
			s.tickAutoAttach()
		}
	}
}

func normalizeExeKey(exePath string) string {
	return strings.ToLower(filepath.Clean(exePath))
}

func (s *Session) recordDetachCooldown(exePath string) {
	if strings.TrimSpace(exePath) == "" {
		return
	}
	s.autoAttachMu.Lock()
	s.detachCooldown[normalizeExeKey(exePath)] = time.Now()
	s.autoAttachMu.Unlock()
}

func (s *Session) clearDetachCooldown(exePath string) {
	if strings.TrimSpace(exePath) == "" {
		return
	}
	s.autoAttachMu.Lock()
	delete(s.detachCooldown, normalizeExeKey(exePath))
	s.autoAttachMu.Unlock()
}

func (s *Session) isOnCooldown(exePath string, now time.Time) bool {
	k := normalizeExeKey(exePath)
	s.autoAttachMu.Lock()
	defer s.autoAttachMu.Unlock()
	at, ok := s.detachCooldown[k]
	if !ok {
		return false
	}
	if now.Sub(at) >= autoAttachDetachCooldown {
		delete(s.detachCooldown, k)
		return false
	}
	return true
}

func (s *Session) tickAutoAttach() {
	if s.host == nil && s.autoAttachFn == nil {
		return
	}
	s.mu.Lock()
	busy := len(s.pids) > 0 || s.gamePath != ""
	s.mu.Unlock()
	if busy {
		return
	}

	games := s.store.Games()
	if len(games) == 0 {
		return
	}
	now := time.Now()
	type candidate struct {
		exePath string
		normExe string
		base    string
	}
	var cands []candidate
	basenames := map[string]bool{}
	for exePath, prof := range games {
		if !prof.AutoAttach {
			continue
		}
		if s.isOnCooldown(exePath, now) {
			continue
		}
		norm := normalizeExeKey(exePath)
		base := strings.ToLower(filepath.Base(filepath.Clean(exePath)))
		cands = append(cands, candidate{exePath: exePath, normExe: norm, base: base})
		basenames[base] = true
	}
	if len(cands) == 0 {
		return
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].normExe < cands[j].normExe })

	scanFn := s.runningPathsFn
	if scanFn == nil {
		scanFn = hook.RunningPathsFor
	}
	running := scanFn(basenames)
	if len(running) == 0 {
		return
	}

	attachFn := s.autoAttachFn
	if attachFn == nil {
		attachFn = s.Attach
	}
	for _, c := range cands {
		pid := running[c.normExe]
		if pid == 0 {
			continue
		}
		if err := attachFn(pid); err != nil {
			// Pause retries on failures (e.g. elevated process requiring admin) to avoid tight error loops.
			s.recordDetachCooldown(c.exePath)
			s.log("warn", fmt.Sprintf("auto-attach %s failed: %v", filepath.Base(c.exePath), err))
			return
		}
		s.log("info", "auto-attached to "+filepath.Base(c.exePath))
		return
	}
}

// SetGameAutoAttach persists per-game AutoAttach setting for exePath.
func (s *Session) SetGameAutoAttach(exePath string, enabled bool) error {
	if strings.TrimSpace(exePath) == "" {
		return errors.New("executable path is required")
	}
	if err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
		}
		g.AutoAttach = enabled
	}); err != nil {
		return err
	}
	if enabled {
		s.clearDetachCooldown(exePath)
		s.log("info", "enabled auto-attach for "+filepath.Base(exePath))
	} else {
		s.log("info", "disabled auto-attach for "+filepath.Base(exePath))
	}
	return nil
}

// Paused reports whether live translation is temporarily paused.
func (s *Session) Paused() bool { return s.paused.Load() }

// TogglePause toggles temporary pass-through mode without detaching hooks.
func (s *Session) TogglePause() bool {
	var next bool
	for {
		cur := s.paused.Load()
		next = !cur
		if s.paused.CompareAndSwap(cur, next) {
			break
		}
	}
	if next {
		s.log("info", "translation paused (hotkey)")
	} else {
		s.log("info", "translation resumed (hotkey)")
	}
	s.post(EventStatus, s.Status())
	return next
}

// CycleDisplayMode advances the active embed display mode (0->1->2->3->0).
func (s *Session) CycleDisplayMode() (hook.DisplayMode, error) {
	s.mu.Lock()
	gamePath := s.gamePath
	s.mu.Unlock()

	if gamePath != "" {
		prof := s.store.Game(gamePath)
		if prof.EmbedOverride != nil {
			ov := *prof.EmbedOverride
			next := (ov.DisplayMode + 1) % 4
			ov.DisplayMode = next
			if err := s.SaveGameOverrides(gamePath, &ov, prof.TranslatorOverride); err != nil {
				return 0, err
			}
			return hook.DisplayMode(next), nil
		}
	}

	cfg := s.store.Config()
	next := (cfg.Embed.DisplayMode + 1) % 4
	cfg.Embed.DisplayMode = next
	if err := s.SaveConfig(cfg); err != nil {
		return 0, err
	}
	return hook.DisplayMode(next), nil
}

// AdjustFontScale adjusts the active relative font scale by delta within [0.5, 2.5].
func (s *Session) AdjustFontScale(delta float64) (float64, error) {
	s.mu.Lock()
	gamePath := s.gamePath
	s.mu.Unlock()

	clampScale := func(cur float32) float32 {
		c := float64(cur)
		if c <= 0 {
			c = 1.0
		}
		next := math.Round((c+delta)*10) / 10
		if next < 0.5 {
			next = 0.5
		} else if next > 2.5 {
			next = 2.5
		}
		return float32(next)
	}

	if gamePath != "" {
		prof := s.store.Game(gamePath)
		if prof.EmbedOverride != nil {
			ov := *prof.EmbedOverride
			ov.ChangeFontSize = true
			ov.FontSizeRelative = clampScale(ov.FontSizeRelative)
			if err := s.SaveGameOverrides(gamePath, &ov, prof.TranslatorOverride); err != nil {
				return 0, err
			}
			return math.Round(float64(ov.FontSizeRelative)*10) / 10, nil
		}
	}

	cfg := s.store.Config()
	cfg.Embed.ChangeFontSize = true
	cfg.Embed.FontSizeRelative = clampScale(cfg.Embed.FontSizeRelative)
	if err := s.SaveConfig(cfg); err != nil {
		return 0, err
	}
	return math.Round(float64(cfg.Embed.FontSizeRelative)*10) / 10, nil
}

func (s *Session) setLastEmbed(text string, tp hook.ThreadParam) {
	s.lastEmbedMu.Lock()
	if strings.TrimSpace(text) == "" {
		s.lastEmbed = nil
	} else {
		s.lastEmbed = &embedJob{text: text, tp: tp}
	}
	s.lastEmbedMu.Unlock()
}

// RetranslateLast forces a fresh (non-cached) translation of the most recently
// processed embedded line and emits the updated line to the live feed.
func (s *Session) RetranslateLast() (EmbedLine, error) {
	s.lastEmbedMu.Lock()
	var job *embedJob
	if s.lastEmbed != nil {
		cp := *s.lastEmbed
		job = &cp
	}
	s.lastEmbedMu.Unlock()
	if job == nil || strings.TrimSpace(job.text) == "" {
		return EmbedLine{}, errors.New("no embedded dialogue line captured yet")
	}

	start := time.Now()
	r := s.rules.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	trans, _, err := s.translateWith(ctx, job.text, s.translator.Active(), func(c context.Context, in string) (string, bool, error) {
		if s.providerBuilder != nil {
			s.mu.Lock()
			gamePath := s.gamePath
			s.mu.Unlock()
			tcfg := s.effectiveTranslator(s.store.Config().Translator, gamePath)
			if p := s.providerBuilder(tcfg); p != nil {
				out, perr := p.Translate(c, in)
				return out, false, perr
			}
		}
		out, ferr := s.translator.TranslateFreshValidated(c, in, func(raw string) error {
			if !pipeline.PlaceholdersPreserved(in, r.post.Apply(raw)) {
				return errors.New("translation dropped [..]/{..} placeholders")
			}
			return nil
		})
		return out, false, ferr
	})
	if err != nil {
		return EmbedLine{}, err
	}

	// Translate via providerBuilder doesn't auto-write cache, whereas TranslateFreshValidated does.
	// Keep cache consistent when using an injected provider in tests too.
	_ = translator.ErrNoProvider
	shaped := s.ShapeForEmbed(trans)
	line := EmbedLine{
		Key:        job.tp.Key(),
		Original:   job.text,
		Translated: trans,
		Shaped:     shaped,
		Cached:     false,
		ElapsedMs:  time.Since(start).Milliseconds(),
	}
	s.post(EventEmbedLine, line)
	s.log("info", "re-translated last line")
	return line, nil
}

// EmitHotkeyToast broadcasts a short status banner notification for global hotkeys.
func (s *Session) EmitHotkeyToast(action, label string) {
	if strings.TrimSpace(label) == "" {
		return
	}
	s.post(EventHotkeyToast, map[string]string{
		"action": action,
		"label":  label,
	})
}
