//go:build windows

package session

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
)

// ---- configuration ----

// Config returns the current settings.
func (s *Session) Config() config.Config { return s.store.Config() }

// SaveConfig validates, persists and applies settings, then pushes embed
// settings to every connected process.
func (s *Session) SaveConfig(c config.Config) error {
	c = normalizeConfig(c)
	prev := s.store.Config()
	if err := s.applyConfig(c); err != nil {
		return err
	}
	if err := s.store.SaveConfig(c); err != nil {
		_ = s.applyConfig(prev)
		return err
	}
	s.mu.Lock()
	pids := s.pidList()
	s.mu.Unlock()
	for _, pid := range pids {
		s.pushEmbedSettings(pid)
	}
	s.post(EventConfigUpdated, c)
	s.post(EventStatus, s.Status())
	return nil
}

func normalizeEmbed(e config.EmbedSettings) config.EmbedSettings {
	if e.TimeoutMs < 100 {
		e.TimeoutMs = 100
	} else if e.TimeoutMs > 30000 {
		e.TimeoutMs = 30000
	}
	if e.MaxLineLength <= 0 {
		e.MaxLineLength = arabic.DefaultLineLength
	}
	if e.FontSizeRelative <= 0 {
		e.FontSizeRelative = 1.0
	}
	return e
}

func normalizeConfig(c config.Config) config.Config {
	c.Embed = normalizeEmbed(c.Embed)
	if c.UI.Language != "ar" && c.UI.Language != "en" {
		c.UI.Language = "ar"
	}
	if c.UI.Mode != "simple" && c.UI.Mode != "advanced" {
		c.UI.Mode = "simple"
	}
	return c
}

func (s *Session) effectiveEmbed(global config.EmbedSettings, gamePath string) config.EmbedSettings {
	if gamePath != "" {
		if eo := s.store.Game(gamePath).EmbedOverride; eo != nil {
			return normalizeEmbed(*eo)
		}
	}
	return normalizeEmbed(global)
}

func (s *Session) effectiveTranslator(global config.TranslatorSettings, gamePath string) config.TranslatorSettings {
	if gamePath != "" {
		if to := s.store.Game(gamePath).TranslatorOverride; to != nil {
			return *to
		}
	}
	return global
}

func (s *Session) applyConfig(c config.Config) error {
	s.mu.Lock()
	gamePath := s.gamePath
	s.mu.Unlock()
	if err := s.rebuildRules(c.Pipeline, gamePath); err != nil {
		return err
	}
	embed := s.effectiveEmbed(c.Embed, gamePath)
	s.embedCfg.Store(&embed)
	s.translator.Configure(s.effectiveTranslator(c.Translator, gamePath))
	return nil
}

func (s *Session) rebuildRules(global config.PipelineSettings, gamePath string) error {
	preList := make([]config.Rule, 0, len(global.PreRules))
	preList = append(preList, global.PreRules...)
	postList := make([]config.Rule, 0, len(global.PostRules))
	postList = append(postList, global.PostRules...)
	var gp config.PipelineSettings
	if gamePath != "" {
		gp = s.store.Game(gamePath).Pipeline
		preList = append(preList, gp.PreRules...)
		postList = append(postList, gp.PostRules...)
	}
	pre, err := pipeline.Compile(preList)
	if err != nil {
		return fmt.Errorf("pre-processing: %w", err)
	}
	post, err := pipeline.Compile(postList)
	if err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	glossary := pipeline.CompileGlossary(global.Glossary, gp.Glossary)
	s.rules.Store(&rules{pre: pre, post: post, glossary: glossary})
	return nil
}

// ---- attach / detach ----

// Attach connects to every running instance of pid's executable and injects khutaf-hook64.dll.
func (s *Session) Attach(pid uint32) error {
	if s.host == nil {
		return fmt.Errorf("hook host unavailable: %v", s.hostErr)
	}
	path, err := hook.ProcessPath(pid)
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	is64, err := hook.Is64Bit(pid)
	if err != nil {
		return fmt.Errorf("query process %d: %w", pid, err)
	}
	if !is64 {
		return errors.New("32-bit games are not supported yet (x64 Unity only)")
	}
	engine := hook.DetectEngine(path)
	if !strings.HasPrefix(engine, "unity") {
		return fmt.Errorf("unsupported engine for %s: only 64-bit Unity games are supported yet", filepath.Base(path))
	}
	pids := hook.PIDsByPath(path)
	if len(pids) == 0 {
		pids = []uint32{pid}
	}

	s.Detach()
	s.mu.Lock()
	s.gamePath = path
	s.is64 = is64
	s.engine = engine
	s.mu.Unlock()
	_ = s.applyConfig(s.store.Config())
	s.translator.SetGame(path)

	var needInject []uint32
	for _, p := range pids {
		s.host.ConnectProcess(p)
		if s.host.CheckIfNeedInject(p) {
			needInject = append(needInject, p)
		}
	}
	s.log("info", fmt.Sprintf("attaching to %s (pids %v, %s)", filepath.Base(path), pids, archName(is64)))
	if ac := hook.DetectAntiCheat(pids, path); ac != "" {
		s.log("warn", fmt.Sprintf("detected %s in %s — injecting into anti-cheat or online games may cause bans or crashes", ac, filepath.Base(path)))
	}
	injectStart := time.Now()
	err = hook.Inject(s.runtimeDir, needInject, is64)
	s.RecordSpan(SubIPCPipe, "hook.Inject", injectStart, err, map[string]any{"pids": needInject})
	if err == nil {
		err = s.host.WaitConnected(pids, 8*time.Second)
	}
	if err != nil {
		for _, p := range pids {
			s.host.DetachProcess(p)
		}
		s.Detach()
		s.log("error", err.Error())
		return err
	}
	s.post(EventStatus, s.Status())
	s.clearDetachCooldown(path)
	return nil
}

// Detach unhooks all connected processes and clears hook state.
func (s *Session) Detach() {
	s.mu.Lock()
	prevPath := s.gamePath
	pids := s.pidList()
	s.pids = map[uint32]bool{}
	s.hooks = map[string]hook.HookInfo{}
	s.hookAddrs = map[string]map[uint32]uint64{}
	s.embedding = map[string]bool{}
	s.lastOutput = map[string]string{}
	s.gamePath = ""
	s.engine = ""
	s.mu.Unlock()
	s.setLastEmbed("", hook.ThreadParam{})
	if prevPath != "" {
		s.recordDetachCooldown(prevPath)
	}
	_ = s.applyConfig(s.store.Config())
	s.translator.SetGame("")
	if s.host != nil {
		for _, pid := range pids {
			s.host.DetachProcess(pid)
		}
	}
	s.post(EventStatus, s.Status())
}
