package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/session"
)

// App is the Wails-bound facade over the session.
type App struct {
	ctx     context.Context
	session *session.Session
}

// NewApp creates a new App.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	store, err := config.Open(configDir())
	if err != nil {
		runtime.LogError(ctx, err.Error())
		// Fall back to a temp dir so the UI still opens.
		store, _ = config.Open(filepath.Join(os.TempDir(), "Khutaf"))
	}
	a.session = session.New(store, session.ResolveRuntimeDir(), func(name string, data any) {
		runtime.EventsEmit(ctx, name, data)
	})
}

func (a *App) shutdown(context.Context) {
	if a.session != nil {
		a.session.Detach()
	}
}

// configDir is <exe dir>/userconfig (portable install).
func configDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "userconfig"
	}
	return filepath.Join(filepath.Dir(exe), "userconfig")
}

// Status returns the session status.
func (a *App) Status() session.Status { return a.session.Status() }

// ListProcesses returns attachable windowed processes.
func (a *App) ListProcesses() ([]hook.ProcessInfo, error) { return a.session.ListProcesses() }

// Attach hooks the game owning pid.
func (a *App) Attach(pid uint32) error { return a.session.Attach(pid) }

// Detach unhooks the current game.
func (a *App) Detach() { a.session.Detach() }

// Hooks returns discovered text hooks.
func (a *App) Hooks() []session.HookView { return a.session.Hooks() }

// SetEmbed toggles in-game embedding for a hook.
func (a *App) SetEmbed(key string, enabled bool) error { return a.session.SetEmbed(key, enabled) }

// InsertHookCode inserts a manual hook code.
func (a *App) InsertHookCode(code string) error { return a.session.InsertHookCode(code) }

// GetConfig returns settings.
func (a *App) GetConfig() config.Config { return a.session.Config() }

// SaveConfig persists settings.
func (a *App) SaveConfig(c config.Config) error { return a.session.SaveConfig(c) }

// ShapePreview is the result of PreviewArabic.
type ShapePreview struct {
	Logical string `json:"logical"`
	Shaped  string `json:"shaped"`
	Codes   string `json:"codes"`
}

// PreviewArabic runs the embed shaping pipeline on text using current settings.
func (a *App) PreviewArabic(text string) ShapePreview {
	shaped := a.session.ShapeForEmbed(text)
	cfg := a.session.Config().Embed
	return ShapePreview{
		Logical: arabic.Wrap(text, cfg.MaxLineLength),
		Shaped:  shaped,
		Codes:   codepoints(shaped),
	}
}

// TranslateResult is the result of TestTranslate.
type TranslateResult struct {
	Translated string `json:"translated"`
	Shaped     string `json:"shaped"`
	Cached     bool   `json:"cached"`
	ElapsedMs  int64  `json:"elapsedMs"`
}

// TestTranslate runs the full translation + shaping pipeline on text.
func (a *App) TestTranslate(text string) (TranslateResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	trans, cached, err := a.session.Translate(ctx, text)
	if err != nil {
		return TranslateResult{}, err
	}
	return TranslateResult{
		Translated: trans,
		Shaped:     a.session.ShapeForEmbed(trans),
		Cached:     cached,
		ElapsedMs:  time.Since(start).Milliseconds(),
	}, nil
}

func codepoints(s string) string {
	out := make([]byte, 0, len(s)*7)
	for i, r := range s {
		if i > 0 {
			out = append(out, ' ')
		}
		if r == '\n' {
			out = append(out, "\\n"...)
			continue
		}
		out = append(out, []byte(hexRune(r))...)
	}
	return string(out)
}

func hexRune(r rune) string {
	const digits = "0123456789ABCDEF"
	buf := []byte{'0', '0', '0', '0'}
	for i := 3; i >= 0; i-- {
		buf[i] = digits[r&0xF]
		r >>= 4
	}
	if r > 0 {
		return string(digits[r&0xF]) + string(buf)
	}
	return string(buf)
}
