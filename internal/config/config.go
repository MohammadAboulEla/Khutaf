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
	LimitLineLength  bool    `json:"limitLineLength"` // wrap non-Arabic output too
	MaxLineLength    int     `json:"maxLineLength"`
}

// HookSettings is passed to Luna_Settings.
type HookSettings struct {
	FlushDelayMs   int `json:"flushDelayMs"`
	Codepage       int `json:"codepage"`
	MaxBufferSize  int `json:"maxBufferSize"`
	MaxHistorySize int `json:"maxHistorySize"`
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

// TranslatorSettings selects and configures the active provider.
type TranslatorSettings struct {
	Active string       `json:"active"` // "bing" | "llm" | "none"
	Bing   BingSettings `json:"bing"`
	LLM    LLMSettings  `json:"llm"`
}

// Rule is a text replacement applied before or after translation.
type Rule struct {
	Enabled bool   `json:"enabled"`
	Regex   bool   `json:"regex"`
	Pattern string `json:"pattern"`
	Replace string `json:"replace"`
}

// PipelineSettings holds text processing rules.
type PipelineSettings struct {
	PreRules  []Rule `json:"preRules"`
	PostRules []Rule `json:"postRules"`
}

// Config is the root of config.json.
type Config struct {
	Embed      EmbedSettings      `json:"embed"`
	Hook       HookSettings       `json:"hook"`
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
	Title      string      `json:"title"`
	EmbedHooks []EmbedHook `json:"embedHooks"`
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
			FontSizeRelative: 1,
			MaxLineLength:    40,
		},
		Hook: HookSettings{
			FlushDelayMs:   500,
			Codepage:       932,
			MaxBufferSize:  3000,
			MaxHistorySize: 1000000,
		},
		Translator: TranslatorSettings{
			Active: "bing",
			Bing: BingSettings{
				SourceLang: "auto-detect",
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
	s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, "config.json"), c)
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
	s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, "games.json"), snapshot)
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
	snapshot := make(map[string]GameProfile, len(s.games))
	for k, v := range s.games {
		snapshot[k] = v
	}
	s.mu.Unlock()
	return writeJSON(filepath.Join(s.dir, "games.json"), snapshot)
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
