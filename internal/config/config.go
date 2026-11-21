// Package config persists app settings (config.json) and per-game profiles (games.json).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EmbedSettings controls in-game embedded translation.
type EmbedSettings struct {
	TimeoutMs        int     `json:"timeoutMs"`
	DisplayMode      int     `json:"displayMode"` // 0 trans, 1 ori+trans, 2 trans+ori, 3 ori
	ClearText        bool    `json:"clearText"`
	ChangeFont       bool    `json:"changeFont"`
	FontFamily       string  `json:"fontFamily"`
	UseUnityBundle   bool    `json:"useUnityBundle"` // load arialuni_sdf_u* (overrides OS font when glyphs exist)
	ChangeFontSize   bool    `json:"changeFontSize"`
	FontSizeRelative float32 `json:"fontSizeRelative"`
	LimitLineLength     bool    `json:"limitLineLength"` // wrap non-Arabic output too
	MaxLineLength       int     `json:"maxLineLength"`
	StripTypewriterTags bool    `json:"stripTypewriterTags"` // strip <color=#00000000> typewriter tags
	NonBlocking         bool    `json:"nonBlocking"`         // return immediately and update next frame
	PendingText         string  `json:"pendingText"`         // empty keeps original while translating
}

// LLMSettings configures an OpenAI-compatible chat completion endpoint.
type LLMSettings struct {
	BaseURL      string  `json:"baseUrl"`
	APIKey       string  `json:"apiKey"`
	Model        string  `json:"model"`
	SystemPrompt string  `json:"systemPrompt"`
	Temperature  float64 `json:"temperature"`
	SourceLang   string  `json:"sourceLang"`
	TargetLang   string  `json:"targetLang"`
}

// BingSettings configures the free Bing web translator.
type BingSettings struct {
	SourceLang string `json:"sourceLang"` // e.g. "auto-detect", "en", "ja"
	TargetLang string `json:"targetLang"` // e.g. "ar"
}

// TraditionalSettings configures additional free traditional web translators.
type TraditionalSettings struct {
	SourceLang string `json:"sourceLang"` // e.g. "auto", "en", "ja"
	TargetLang string `json:"targetLang"` // e.g. "ar"
}

// LlamaCppSettings configures the local llama-server.exe runner.
type LlamaCppSettings struct {
	BinaryPath string `json:"binaryPath"`
	ModelPath  string `json:"modelPath"`
	Port       int    `json:"port"`
	GPULayers  int    `json:"gpuLayers"`
	CtxSize    int    `json:"ctxSize"`
	AutoStart  bool   `json:"autoStart"`
	ExtraArgs  string `json:"extraArgs"`
}

// OfflineLLMSettings configures local offline translation GGUF prompt templates.
type OfflineLLMSettings struct {
	BaseURL     string  `json:"baseUrl"`
	Model       string  `json:"model"`
	Template    string  `json:"template"` // "auto" | "hymt2" | "translate_gemma" | "nllb_instruct"
	SourceLang  string  `json:"sourceLang"`
	TargetLang  string  `json:"targetLang"`
	Temperature float64 `json:"temperature"`
}

// TranslatorSettings selects and configures the active provider.
type TranslatorSettings struct {
	Active           string              `json:"active"` // "bing" | "google" | ... | "llm" | "offline_llm" | "none"
	FallbackProvider string              `json:"fallbackProvider,omitempty"`
	Bing             BingSettings        `json:"bing"`
	Traditional      TraditionalSettings `json:"traditional"`
	LLM              LLMSettings         `json:"llm"`
	OfflineLLM       OfflineLLMSettings  `json:"offlineLlm"`
	LlamaCpp         LlamaCppSettings    `json:"llamaCpp"`
}

// Rule is a text replacement applied before or after translation.
type Rule struct {
	Enabled bool   `json:"enabled"`
	Regex   bool   `json:"regex"`
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
}

// GlossaryEntry maps a source proper noun or term to its target Arabic translation.
type GlossaryEntry struct {
	Enabled       bool   `json:"enabled"`
	Source        string `json:"source"`
	Target        string `json:"target"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
}

// GlossaryFile is the on-disk schema of glossary.json.
type GlossaryFile struct {
	Global []GlossaryEntry            `json:"global"`
	Games  map[string][]GlossaryEntry `json:"games,omitempty"`
}

// PipelineSettings holds text processing rules and glossary terms.
type PipelineSettings struct {
	PreRules  []Rule          `json:"preRules"`
	PostRules []Rule          `json:"postRules"`
	Glossary  []GlossaryEntry `json:"glossary,omitempty"`
}

// UISettings stores user-interface preferences.
type UISettings struct {
	Language  string `json:"language"`  // "ar" | "en"
	Mode      string `json:"mode"`      // "simple" | "advanced"
	Onboarded bool   `json:"onboarded"` // true after first-run wizard completes or is skipped
}

// HotkeySettings configures global Windows shortcuts and system tray behavior.
type HotkeySettings struct {
	Enabled         bool   `json:"enabled"`
	MinimizeToTray  bool   `json:"minimizeToTray"`
	CycleMode       string `json:"cycleMode"`
	TogglePause     string `json:"togglePause"`
	Retranslate     string `json:"retranslate"`
	FontScaleUp     string `json:"fontScaleUp"`
	FontScaleDown   string `json:"fontScaleDown"`
	ToggleHooksMenu string `json:"toggleHooksMenu"`
	PopupW          int    `json:"popupW,omitempty"`
	PopupH          int    `json:"popupH,omitempty"`
	PopupX          int    `json:"popupX,omitempty"`
	PopupY          int    `json:"popupY,omitempty"`
	PopupHasPos     bool   `json:"popupHasPos,omitempty"`
}

// Config is the root of config.json.
type Config struct {
	Embed      EmbedSettings      `json:"embed"`
	Translator TranslatorSettings `json:"translator"`
	Pipeline   PipelineSettings   `json:"pipeline"`
	UI         UISettings         `json:"ui"`
	Hotkeys    HotkeySettings     `json:"hotkeys"`
}

// EmbedHook identifies a hook whose embedding is re-enabled on reattach.
type EmbedHook struct {
	HookCode string `json:"hookCode"`
	HookName string `json:"hookName"`
	Ctx      uint64 `json:"ctx"`
	Ctx2     uint64 `json:"ctx2"`
}

// GameProfile stores per-game state keyed by executable path.
type GameProfile struct {
	Title              string              `json:"title"`
	EmbedHooks         []EmbedHook         `json:"embedHooks"`
	Pipeline           PipelineSettings    `json:"pipeline,omitempty"`
	EmbedOverride      *EmbedSettings      `json:"embedOverride,omitempty"`
	TranslatorOverride *TranslatorSettings `json:"translatorOverride,omitempty"`
	AutoAttach         bool                `json:"autoAttach,omitempty"`
}

// Default returns the factory configuration.
func Default() Config {
	return Config{
		Embed: EmbedSettings{
			TimeoutMs:        3000,
			DisplayMode:      1,
			ChangeFont:       true,
			FontFamily:       "Tahoma",
			UseUnityBundle:   true,
			FontSizeRelative:    1,
			MaxLineLength:       40,
			StripTypewriterTags: true,
			NonBlocking:         true,
			PendingText:         "",
		},
		Translator: TranslatorSettings{
			Active: "bing",
			Bing: BingSettings{
				SourceLang: "auto-detect",
				TargetLang: "ar",
			},
			Traditional: TraditionalSettings{
				SourceLang: "auto",
				TargetLang: "ar",
			},
			LLM: LLMSettings{
				BaseURL:     "https://api.openai.com/v1",
				Model:       "gpt-4o-mini",
				Temperature: 0.2,
				SourceLang:  "English",
				TargetLang:  "Arabic",
				SystemPrompt: "You are a professional video game localizer. Translate the user's {source} game text into natural, concise {target}. " +
					"Keep every tag like <...>, [...], {...} and placeholders exactly unchanged. Output only the translation.",
			},
			OfflineLLM: OfflineLLMSettings{
				BaseURL:     "http://127.0.0.1:8080/v1",
				Model:       "local-model",
				Template:    "auto",
				SourceLang:  "English",
				TargetLang:  "Arabic",
				Temperature: 0.1,
			},
			LlamaCpp: LlamaCppSettings{
				Port:      8080,
				GPULayers: 99,
				CtxSize:   4096,
			},
		},
		UI: UISettings{
			Language:  "ar",
			Mode:      "simple",
			Onboarded: false,
		},
		Hotkeys: HotkeySettings{
			Enabled:         true,
			MinimizeToTray:  false,
			CycleMode:       "Ctrl+Alt+M",
			TogglePause:     "Ctrl+Alt+P",
			Retranslate:     "Ctrl+Alt+R",
			FontScaleUp:     "Ctrl+Alt+Up",
			FontScaleDown:   "Ctrl+Alt+Down",
			ToggleHooksMenu: "Ctrl+Alt+H",
		},
	}
}

// Store loads and saves config.json and games.json under dir.
type Store struct {
	dir      string
	mu       sync.RWMutex
	ioMu     sync.Mutex
	cfg      Config
	games    map[string]GameProfile
	initWarn string
}

// NewMemoryStore creates an in-memory Store with default settings and no disk backing.
func NewMemoryStore() *Store {
	return &Store{cfg: Default(), games: map[string]GameProfile{}}
}

// InitWarning returns any non-fatal warning encountered while opening the store.
func (s *Store) InitWarning() string { return s.initWarn }

// SetInitWarning sets or appends a non-fatal startup warning for display in the UI.
func (s *Store) SetInitWarning(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	if s.initWarn == "" {
		s.initWarn = msg
		return
	}
	s.initWarn += "; " + msg
}

// Open loads (or initializes) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	// Probe writability up front so read-only locations fail even when config.json already exists.
	probe, err := os.CreateTemp(dir, ".khutaf-write-probe-*")
	if err != nil {
		return nil, fmt.Errorf("config dir not writable (%s): %w", dir, err)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	s := &Store{dir: dir, cfg: Default(), games: map[string]GameProfile{}}
	cfgPath := filepath.Join(dir, "config.json")
	if err := readJSON(cfgPath, &s.cfg); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		if errors.As(err, &syn) || errors.As(err, &typ) {
			badPath := fmt.Sprintf("%s.bad-%s", cfgPath, time.Now().Format("20060102-150405"))
			if rerr := os.Rename(cfgPath, badPath); rerr == nil {
				s.initWarn = fmt.Sprintf("corrupt config.json backed up to %s; loaded defaults", filepath.Base(badPath))
			} else {
				s.initWarn = fmt.Sprintf("corrupt config.json (%v); loaded defaults", err)
			}
			s.cfg = Default()
		} else {
			return nil, err
		}
	}
	if s.cfg.Translator.LLM.APIKey != "" {
		plain, err := unprotectSecret(s.cfg.Translator.LLM.APIKey)
		if err != nil {
			s.cfg.Translator.LLM.APIKey = ""
			s.SetInitWarning("stored LLM API key could not be decrypted on this Windows account; please re-enter it in Settings")
		} else {
			s.cfg.Translator.LLM.APIKey = plain
		}
	}
	if err := readJSON(filepath.Join(dir, "games.json"), &s.games); err != nil {
		return nil, err
	}
	for k, g := range s.games {
		if g.TranslatorOverride != nil && g.TranslatorOverride.LLM.APIKey != "" {
			plain, err := unprotectSecret(g.TranslatorOverride.LLM.APIKey)
			if err != nil {
				g.TranslatorOverride.LLM.APIKey = ""
				s.SetInitWarning(fmt.Sprintf("stored per-game LLM API key for %s could not be decrypted on this Windows account", filepath.Base(k)))
			} else {
				g.TranslatorOverride.LLM.APIKey = plain
			}
			s.games[k] = g
		}
	}
	var gf GlossaryFile
	if err := readJSON(filepath.Join(dir, "glossary.json"), &gf); err != nil {
		return nil, err
	}
	if len(gf.Global) > 0 {
		s.cfg.Pipeline.Glossary = gf.Global
	}
	for k, entries := range gf.Games {
		if len(entries) == 0 {
			continue
		}
		key := gameKey(k)
		g := s.games[key]
		g.Pipeline.Glossary = entries
		s.games[key] = g
	}
	return s, nil
}

// Dir returns the storage folder.
func (s *Store) Dir() string { return s.dir }

// Config returns a copy of the current config.
func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

// SaveConfig replaces and persists the config.
func (s *Store) SaveConfig(c Config) error {
	c = cloneConfig(c)
	diskCfg := cloneConfig(c)
	if s.dir != "" && diskCfg.Translator.LLM.APIKey != "" {
		enc, err := protectSecret(diskCfg.Translator.LLM.APIKey)
		if err != nil {
			return fmt.Errorf("protect LLM API key: %w", err)
		}
		diskCfg.Translator.LLM.APIKey = enc
	}
	if s.dir != "" {
		s.ioMu.Lock()
		defer s.ioMu.Unlock()
	}
	s.mu.Lock()
	s.cfg = c
	games := s.gamesSnapshotLocked()
	s.mu.Unlock()
	if s.dir == "" {
		return nil
	}
	if err := writeJSON(filepath.Join(s.dir, "config.json"), diskCfg); err != nil {
		return err
	}
	return s.saveGlossaryFile(c.Pipeline.Glossary, games)
}

// Game returns the profile for exePath.
func (s *Store) Game(exePath string) GameProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneGameProfile(s.games[gameKey(exePath)])
}

// Games returns a snapshot of all saved game profiles keyed by normalized exe path.
func (s *Store) Games() map[string]GameProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gamesSnapshotLocked()
}

// DeleteGame removes a saved game profile and persists games.json.
func (s *Store) DeleteGame(exePath string) error {
	if s.dir != "" {
		s.ioMu.Lock()
		defer s.ioMu.Unlock()
	}
	s.mu.Lock()
	delete(s.games, gameKey(exePath))
	snapshot := s.gamesSnapshotLocked()
	globalGlossary := cloneGlossary(s.cfg.Pipeline.Glossary)
	s.mu.Unlock()
	if s.dir == "" {
		return nil
	}
	diskGames, err := protectGamesForDisk(snapshot)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.dir, "games.json"), diskGames); err != nil {
		return err
	}
	return s.saveGlossaryFile(globalGlossary, snapshot)
}

func (s *Store) gamesSnapshotLocked() map[string]GameProfile {
	snapshot := make(map[string]GameProfile, len(s.games))
	for k, v := range s.games {
		snapshot[k] = cloneGameProfile(v)
	}
	return snapshot
}

// UpdateGame mutates and persists the profile for exePath.
func (s *Store) UpdateGame(exePath string, fn func(*GameProfile)) error {
	if s.dir != "" {
		s.ioMu.Lock()
		defer s.ioMu.Unlock()
	}
	s.mu.Lock()
	key := gameKey(exePath)
	g := cloneGameProfile(s.games[key])
	fn(&g)
	s.games[key] = cloneGameProfile(g)
	snapshot := s.gamesSnapshotLocked()
	globalGlossary := cloneGlossary(s.cfg.Pipeline.Glossary)
	s.mu.Unlock()
	if s.dir == "" {
		return nil
	}
	diskGames, err := protectGamesForDisk(snapshot)
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.dir, "games.json"), diskGames); err != nil {
		return err
	}
	return s.saveGlossaryFile(globalGlossary, snapshot)
}

func clonePipeline(p PipelineSettings) PipelineSettings {
	if p.PreRules != nil {
		r := make([]Rule, len(p.PreRules))
		copy(r, p.PreRules)
		p.PreRules = r
	}
	if p.PostRules != nil {
		r := make([]Rule, len(p.PostRules))
		copy(r, p.PostRules)
		p.PostRules = r
	}
	p.Glossary = cloneGlossary(p.Glossary)
	return p
}

func cloneGlossary(g []GlossaryEntry) []GlossaryEntry {
	if g == nil {
		return nil
	}
	out := make([]GlossaryEntry, len(g))
	copy(out, g)
	return out
}

func cloneConfig(c Config) Config {
	c.Pipeline = clonePipeline(c.Pipeline)
	return c
}

func cloneGameProfile(g GameProfile) GameProfile {
	if g.EmbedHooks != nil {
		hooks := make([]EmbedHook, len(g.EmbedHooks))
		copy(hooks, g.EmbedHooks)
		g.EmbedHooks = hooks
	}
	g.Pipeline = clonePipeline(g.Pipeline)
	if g.EmbedOverride != nil {
		eo := *g.EmbedOverride
		g.EmbedOverride = &eo
	}
	if g.TranslatorOverride != nil {
		to := *g.TranslatorOverride
		g.TranslatorOverride = &to
	}
	return g
}

func protectGamesForDisk(games map[string]GameProfile) (map[string]GameProfile, error) {
	out := make(map[string]GameProfile, len(games))
	for k, g := range games {
		cp := cloneGameProfile(g)
		if cp.TranslatorOverride != nil && cp.TranslatorOverride.LLM.APIKey != "" {
			enc, err := protectSecret(cp.TranslatorOverride.LLM.APIKey)
			if err != nil {
				return nil, fmt.Errorf("protect per-game LLM API key: %w", err)
			}
			cp.TranslatorOverride.LLM.APIKey = enc
		}
		out[k] = cp
	}
	return out, nil
}

func (s *Store) saveGlossaryFile(global []GlossaryEntry, games map[string]GameProfile) error {
	gf := GlossaryFile{Global: global}
	if gf.Global == nil {
		gf.Global = []GlossaryEntry{}
	}
	for k, g := range games {
		if len(g.Pipeline.Glossary) == 0 {
			continue
		}
		if gf.Games == nil {
			gf.Games = make(map[string][]GlossaryEntry)
		}
		gf.Games[k] = g.Pipeline.Glossary
	}
	return writeJSON(filepath.Join(s.dir, "glossary.json"), gf)
}

func gameKey(exePath string) string { return strings.ToLower(filepath.Clean(exePath)) }

func readJSON(path string, dst any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// writeJSON writes atomically via a temp file + rename.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	cleanup = false
	return nil
}
