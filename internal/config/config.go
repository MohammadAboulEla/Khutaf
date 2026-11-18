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
	Active      string              `json:"active"` // "bing" | "google" | ... | "llm" | "offline_llm" | "none"
	Bing        BingSettings        `json:"bing"`
	Traditional TraditionalSettings `json:"traditional"`
	LLM         LLMSettings         `json:"llm"`
	OfflineLLM  OfflineLLMSettings  `json:"offlineLlm"`
	LlamaCpp    LlamaCppSettings    `json:"llamaCpp"`
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

// Config is the root of config.json.
type Config struct {
	Embed      EmbedSettings      `json:"embed"`
	Translator TranslatorSettings `json:"translator"`
	Pipeline   PipelineSettings   `json:"pipeline"`
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
	Title      string           `json:"title"`
	EmbedHooks []EmbedHook      `json:"embedHooks"`
	Pipeline   PipelineSettings `json:"pipeline,omitempty"`
}

// Default returns the factory configuration.
func Default() Config {
	return Config{
		Embed: EmbedSettings{
			TimeoutMs:        3000,
			DisplayMode:      1,
			ChangeFont:       true,
			FontFamily:       "Tahoma",
			UseUnityBundle:   false,
			FontSizeRelative:    1,
			MaxLineLength:       40,
			StripTypewriterTags: true,
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
	}
}

// Store loads and saves config.json and games.json under dir.
type Store struct {
	dir   string
	mu    sync.RWMutex
	cfg   Config
	games map[string]GameProfile
}

// Open loads (or initializes) the store in dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	s := &Store{dir: dir, cfg: Default(), games: map[string]GameProfile{}}
	if err := readJSON(filepath.Join(dir, "config.json"), &s.cfg); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "games.json"), &s.games); err != nil {
		return nil, err
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
	return s.cfg
}

// SaveConfig replaces and persists the config.
func (s *Store) SaveConfig(c Config) error {
	s.mu.Lock()
	s.cfg = c
	games := s.gamesSnapshotLocked()
	s.mu.Unlock()
	if err := writeJSON(filepath.Join(s.dir, "config.json"), c); err != nil {
		return err
	}
	return s.saveGlossaryFile(c.Pipeline.Glossary, games)
}

// Game returns the profile for exePath.
func (s *Store) Game(exePath string) GameProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.games[gameKey(exePath)]
}

// Games returns a snapshot of all saved game profiles keyed by normalized exe path.
func (s *Store) Games() map[string]GameProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]GameProfile, len(s.games))
	for k, v := range s.games {
		hooks := make([]EmbedHook, len(v.EmbedHooks))
		copy(hooks, v.EmbedHooks)
		v.EmbedHooks = hooks
		out[k] = v
	}
	return out
}

// DeleteGame removes a saved game profile and persists games.json.
func (s *Store) DeleteGame(exePath string) error {
	s.mu.Lock()
	delete(s.games, gameKey(exePath))
	snapshot := s.gamesSnapshotLocked()
	globalGlossary := s.cfg.Pipeline.Glossary
	s.mu.Unlock()
	if err := writeJSON(filepath.Join(s.dir, "games.json"), snapshot); err != nil {
		return err
	}
	return s.saveGlossaryFile(globalGlossary, snapshot)
}

func (s *Store) gamesSnapshotLocked() map[string]GameProfile {
	snapshot := make(map[string]GameProfile, len(s.games))
	for k, v := range s.games {
		snapshot[k] = v
	}
	return snapshot
}

// UpdateGame mutates and persists the profile for exePath.
func (s *Store) UpdateGame(exePath string, fn func(*GameProfile)) error {
	s.mu.Lock()
	key := gameKey(exePath)
	g := s.games[key]
	fn(&g)
	s.games[key] = g
	snapshot := s.gamesSnapshotLocked()
	globalGlossary := s.cfg.Pipeline.Glossary
	s.mu.Unlock()
	if err := writeJSON(filepath.Join(s.dir, "games.json"), snapshot); err != nil {
		return err
	}
	return s.saveGlossaryFile(globalGlossary, snapshot)
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Rename(tmp, path)
}
