package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/session"
)

// AppVersion is the single source of truth for the application version.
// Can be overridden at build time via: -ldflags "-X main.AppVersion=x.y.z"
var AppVersion = "0.1.0"

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

// AppVersion returns the application version string.
func (a *App) AppVersion() string { return AppVersion }

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

// SetDebugMode toggles performance profiling mode.
func (a *App) SetDebugMode(enabled bool, realTrans bool) {
	a.session.SetDebugMode(enabled, realTrans)
}

// SavedGames returns all saved game profiles with running/attached status.
func (a *App) SavedGames() []session.SavedGame { return a.session.SavedGames() }

// QuickAttach attaches to a saved game by executable path if it is running.
func (a *App) QuickAttach(exePath string) error { return a.session.QuickAttach(exePath) }

// DeleteSavedGame removes a saved game profile.
func (a *App) DeleteSavedGame(exePath string) error { return a.session.DeleteSavedGame(exePath) }

// ClearSavedGameHooks clears all saved embed hooks for a game.
func (a *App) ClearSavedGameHooks(exePath string) error {
	return a.session.ClearSavedGameHooks(exePath)
}

// RemoveSavedGameHook removes one saved embed hook from a game profile.
func (a *App) RemoveSavedGameHook(exePath, hookCode string, ctx, ctx2 uint64) error {
	return a.session.RemoveSavedGameHook(exePath, hookCode, ctx, ctx2)
}

// SaveGamePipeline saves per-game pre/post rules.
func (a *App) SaveGamePipeline(exePath string, p config.PipelineSettings) error {
	return a.session.SaveGamePipeline(exePath, p)
}

// ClearTranslationCache clears the in-memory translation cache.
func (a *App) ClearTranslationCache() int { return a.session.ClearTranslationCache() }

// CacheCount returns the number of cached translations in memory.
func (a *App) CacheCount() int { return a.session.CacheCount() }

var (
	user32DLL             = syscall.NewLazyDLL("user32.dll")
	gdi32DLL              = syscall.NewLazyDLL("gdi32.dll")
	procGetDC             = user32DLL.NewProc("GetDC")
	procReleaseDC         = user32DLL.NewProc("ReleaseDC")
	procEnumFontFamiliesW = gdi32DLL.NewProc("EnumFontFamiliesExW")
)

type logFontW struct {
	Height         int32
	Width          int32
	Escapement     int32
	Orientation    int32
	Weight         int32
	Italic         byte
	Underline      byte
	StrikeOut      byte
	CharSet        byte
	OutPrecision   byte
	ClipPrecision  byte
	Quality        byte
	PitchAndFamily byte
	FaceName       [32]uint16
}

// ListFonts returns installed Windows font family names.
func (a *App) ListFonts() []string {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return []string{"Arial", "Segoe UI", "Tahoma"}
	}
	defer procReleaseDC.Call(0, hdc)

	seen := map[string]bool{}
	cb := syscall.NewCallback(func(lf *logFontW, _ uintptr, _ uintptr, _ uintptr) uintptr {
		name := windows.UTF16ToString(lf.FaceName[:])
		if name != "" && !strings.HasPrefix(name, "@") {
			seen[name] = true
		}
		return 1
	})
	lf := logFontW{CharSet: 1} // DEFAULT_CHARSET
	procEnumFontFamiliesW.Call(hdc, uintptr(unsafe.Pointer(&lf)), cb, 0, 0)

	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

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
