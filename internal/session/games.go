//go:build windows

package session

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
)

// SavedGame describes a persisted game profile and whether it is currently running.
type SavedGame struct {
	ExePath            string                     `json:"exePath"`
	Title              string                     `json:"title"`
	EmbedHooks         []config.EmbedHook         `json:"embedHooks"`
	Pipeline           config.PipelineSettings    `json:"pipeline"`
	EmbedOverride      *config.EmbedSettings      `json:"embedOverride,omitempty"`
	TranslatorOverride *config.TranslatorSettings `json:"translatorOverride,omitempty"`
	AutoAttach         bool                       `json:"autoAttach"`
	RunningPID         uint32                     `json:"runningPid"`
	Attached           bool                       `json:"attached"`
}

// SavedGames returns all saved game profiles with live running/attached status.
func (s *Session) SavedGames() []SavedGame {
	games := s.store.Games()
	s.mu.Lock()
	rawGamePath := s.gamePath
	isAttached := len(s.pids) > 0
	s.mu.Unlock()
	curPath := ""
	if rawGamePath != "" {
		curPath = strings.ToLower(filepath.Clean(rawGamePath))
	}

	basenames := make(map[string]bool, len(games))
	for exePath := range games {
		basenames[strings.ToLower(filepath.Base(filepath.Clean(exePath)))] = true
	}
	runningByPath := hook.RunningPathsFor(basenames)

	out := make([]SavedGame, 0, len(games))
	for exePath, prof := range games {
		normExe := strings.ToLower(filepath.Clean(exePath))
		runningPID := runningByPath[normExe]
		title := prof.Title
		if title == "" {
			title = filepath.Base(exePath)
		}
		out = append(out, SavedGame{
			ExePath:            exePath,
			Title:              title,
			EmbedHooks:         prof.EmbedHooks,
			Pipeline:           prof.Pipeline,
			EmbedOverride:      prof.EmbedOverride,
			TranslatorOverride: prof.TranslatorOverride,
			AutoAttach:         prof.AutoAttach,
			RunningPID:         runningPID,
			Attached:           isAttached && curPath != "" && curPath == normExe,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Attached != out[j].Attached {
			return out[i].Attached
		}
		if (out[i].RunningPID != 0) != (out[j].RunningPID != 0) {
			return out[i].RunningPID != 0
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// QuickAttach attaches to the first running instance of exePath.
func (s *Session) QuickAttach(exePath string) error {
	pids := hook.PIDsByPath(exePath)
	if len(pids) == 0 {
		return fmt.Errorf("%s is not currently running", filepath.Base(exePath))
	}
	return s.Attach(pids[0])
}

// DeleteSavedGame removes a saved game profile from games.json.
func (s *Session) DeleteSavedGame(exePath string) error {
	return s.store.DeleteGame(exePath)
}

// ClearSavedGameHooks removes all saved embed hooks for exePath.
func (s *Session) ClearSavedGameHooks(exePath string) error {
	return s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		g.EmbedHooks = nil
	})
}

// RemoveSavedGameHook removes a single saved embed hook from exePath and disables it live when attached.
func (s *Session) RemoveSavedGameHook(exePath, hookCode string, ctx, ctx2 uint64) error {
	err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		kept := g.EmbedHooks[:0]
		for _, h := range g.EmbedHooks {
			if h.HookCode == hookCode && h.Ctx == ctx && h.Ctx2 == ctx2 {
				continue
			}
			kept = append(kept, h)
		}
		g.EmbedHooks = kept
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	attachedMatch := s.gamePath != "" && strings.EqualFold(filepath.Clean(s.gamePath), filepath.Clean(exePath))
	var tps []hook.ThreadParam
	var liveDisabled []hook.HookInfo
	if attachedMatch {
		seenTP := map[string]bool{}
		for _, h := range s.hooks {
			if h.HookCode == hookCode && h.Ctx == ctx && h.Ctx2 == ctx2 {
				delete(s.embedding, h.Key)
				liveDisabled = append(liveDisabled, h)
				if !seenTP[h.Key] {
					seenTP[h.Key] = true
					tps = append(tps, h.TP)
				}
			}
		}
		for pid, addr := range s.hookAddrs[hookCode] {
			tp := hook.ThreadParam{ProcessID: pid, Addr: addr, Ctx: ctx, Ctx2: ctx2}
			k := tp.Key()
			delete(s.embedding, k)
			if !seenTP[k] {
				seenTP[k] = true
				tps = append(tps, tp)
			}
		}
	}
	s.mu.Unlock()

	if s.host != nil {
		for _, tp := range tps {
			s.host.UseEmbed(tp, false)
		}
	}
	for _, h := range liveDisabled {
		s.post(EventHookNew, HookView{HookInfo: h, Embedding: false})
	}
	return nil
}

// SaveGamePipeline validates, persists, and (if currently attached) applies per-game rules.
func (s *Session) SaveGamePipeline(exePath string, p config.PipelineSettings) error {
	if _, err := pipeline.Compile(p.PreRules); err != nil {
		return fmt.Errorf("pre-processing: %w", err)
	}
	if _, err := pipeline.Compile(p.PostRules); err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	if err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
		}
		g.Pipeline = p
	}); err != nil {
		return err
	}
	s.mu.Lock()
	curPath := s.gamePath
	s.mu.Unlock()
	if curPath != "" && strings.EqualFold(filepath.Clean(curPath), filepath.Clean(exePath)) {
		return s.rebuildRules(s.store.Config().Pipeline, curPath)
	}
	return nil
}

// SaveGameOverrides saves optional per-game embed/translator overrides (pass nil to inherit global)
// and applies them immediately when exePath is currently attached.
func (s *Session) SaveGameOverrides(exePath string, embedOverride *config.EmbedSettings, translatorOverride *config.TranslatorSettings) error {
	var normEmbed *config.EmbedSettings
	if embedOverride != nil {
		e := normalizeEmbed(*embedOverride)
		normEmbed = &e
	}
	if err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
		}
		g.EmbedOverride = normEmbed
		g.TranslatorOverride = translatorOverride
	}); err != nil {
		return err
	}
	s.mu.Lock()
	curPath := s.gamePath
	pids := s.pidList()
	s.mu.Unlock()
	if curPath != "" && strings.EqualFold(filepath.Clean(curPath), filepath.Clean(exePath)) {
		if err := s.applyConfig(s.store.Config()); err != nil {
			return err
		}
		for _, pid := range pids {
			s.pushEmbedSettings(pid)
		}
		s.post(EventStatus, s.Status())
	}
	return nil
}
