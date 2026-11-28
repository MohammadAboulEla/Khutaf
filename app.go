//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"khutaf/internal/arabic"
	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/hotkeys"
	"khutaf/internal/pipeline"
	"khutaf/internal/session"
	"khutaf/internal/translator"
	"khutaf/internal/updater"
)

// AppVersion is the single source of truth for the application version.
// Can be overridden at build time via: -ldflags "-X main.AppVersion=x.y.z"
var AppVersion = "0.2.5"

// App is the Wails-bound facade over the session.
type App struct {
	ctx             context.Context
	session         *session.Session
	hotkeys         *hotkeys.Manager
	tray            *trayManager
	updater         *updater.Manager
	updatedFrom     string
	updateMu        sync.Mutex
	updateStop      chan struct{}
	updateStopOnce  sync.Once
	quitting        atomic.Bool
	windowHidden    atomic.Bool
	hooksPopupOpen  atomic.Bool
	hooksStandalone atomic.Bool
	openedByHotkey  atomic.Bool

	winMu                  sync.Mutex
	savedW, savedH         int
	savedX, savedY         int
	savedMax, hasSavedGeom bool
	pendingRestoreGeom     bool
}

// NewApp creates a new App.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	primaryDir := configDir()
	store, err := config.Open(primaryDir)
	if err != nil {
		runtime.LogError(ctx, err.Error())
		tempDir := filepath.Join(os.TempDir(), "Khutaf")
		if fallback, ferr := config.Open(tempDir); ferr == nil {
			fallback.SetInitWarning(fmt.Sprintf("config folder %s is not writable (%v); using temporary fallback %s", primaryDir, err, tempDir))
			store = fallback
		} else {
			mem := config.NewMemoryStore()
			mem.SetInitWarning(fmt.Sprintf("config folder %s is not writable (%v); running with in-memory defaults (settings will not persist)", primaryDir, err))
			store = mem
		}
	}
	a.session = session.New(store, session.ResolveRuntimeDir(), func(name string, data any) {
		runtime.EventsEmit(ctx, name, data)
	})
	a.hotkeys = hotkeys.NewManager(a.handleHotkey)
	a.applyHotkeys(store.Config().Hotkeys)
	a.tray = startTrayManager(TrayCallbacks{
		State:       a.trayState,
		ToggleShow:  a.ToggleWindowVisibility,
		TogglePause: func() { a.TogglePause() },
		OpenHooks:   func() { a.openStandaloneHooksPopup() },
		Detach:      func() { a.Detach() },
		Quit:        func() { a.QuitApp() },
	})

	exeDir := installDir()
	updater.CleanupOldFiles(exeDir)
	a.updater = updater.New(
		AppVersion,
		exeDir,
		func(name string, data any) { runtime.EventsEmit(ctx, name, data) },
		func() config.UpdateSettings { return a.session.Config().Update },
		func(lastCheckedAt, etag string) {
			a.updateMu.Lock()
			defer a.updateMu.Unlock()
			cfg := a.session.Config()
			if cfg.Update.LastCheckedAt == lastCheckedAt && cfg.Update.ETag == etag {
				return
			}
			cfg.Update.LastCheckedAt = lastCheckedAt
			cfg.Update.ETag = etag
			_ = a.session.SaveConfig(cfg)
		},
	)
	if a.updatedFrom != "" {
		a.updater.SetUpdatedFrom(a.updatedFrom)
	}
	if uerr := updater.ReadAndClearUpdateError(exeDir); uerr != "" {
		runtime.LogError(ctx, "updater: "+uerr)
		a.updater.SetStartupError(uerr)
	}
	a.updateStop = make(chan struct{})
	go a.updateLoop(ctx, a.updateStop)
}

func (a *App) beforeClose(context.Context) bool {
	if a.quitting.Load() || a.session == nil {
		return false
	}
	if a.session.Config().Hotkeys.MinimizeToTray {
		a.HideToTray()
		return true
	}
	return false
}

func (a *App) shutdown(context.Context) {
	if a.updateStop != nil {
		a.updateStopOnce.Do(func() { close(a.updateStop) })
	}
	if a.hooksStandalone.Load() {
		a.saveStandaloneHooksGeometry()
	}
	if a.hotkeys != nil {
		a.hotkeys.Close()
	}
	if a.tray != nil {
		a.tray.Close()
	}
	if a.session != nil {
		_ = a.session.Close()
	}
}

func installDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
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

// GetConfig returns settings.
func (a *App) GetConfig() config.Config { return a.session.Config() }

// SaveConfig persists settings and applies global hotkey bindings.
func (a *App) SaveConfig(c config.Config) error {
	if err := a.session.SaveConfig(c); err != nil {
		return err
	}
	a.applyHotkeys(c.Hotkeys)
	return nil
}

// TogglePause pauses or resumes live translation without detaching hooks.
func (a *App) TogglePause() bool { return a.session.TogglePause() }

// SetGameAutoAttach toggles automatic re-attachment when exePath launches.
func (a *App) SetGameAutoAttach(exePath string, enabled bool) error {
	return a.session.SetGameAutoAttach(exePath, enabled)
}

// SetDebugMode toggles performance profiling mode.
func (a *App) SetDebugMode(enabled bool, realTrans bool) {
	a.session.SetDebugMode(enabled, realTrans)
}

// GetTelemetrySnapshot returns the telemetry ring buffer and function health matrix.
func (a *App) GetTelemetrySnapshot() session.TelemetrySnapshot {
	return a.session.GetTelemetrySnapshot()
}

// ClearTelemetry resets the telemetry ring buffer and function health stats.
func (a *App) ClearTelemetry() {
	a.session.ClearTelemetry()
}

// ExportDiagnosticBundle writes a .jsonl diagnostic report and returns its file path.
func (a *App) ExportDiagnosticBundle() (string, error) {
	return a.session.ExportDiagnosticBundle()
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

// SaveGameOverrides saves optional per-game embed and translator overrides (nil inherits global).
func (a *App) SaveGameOverrides(exePath string, embedOverride *config.EmbedSettings, translatorOverride *config.TranslatorSettings) error {
	return a.session.SaveGameOverrides(exePath, embedOverride, translatorOverride)
}

// ClearTranslationCache clears non-manual cached translations.
func (a *App) ClearTranslationCache() int { return a.session.ClearTranslationCache() }

// CacheCount returns the total number of cached translations.
func (a *App) CacheCount() int { return a.session.CacheCount() }

// ListCacheEntries returns filtered SQLite cache entries.
func (a *App) ListCacheEntries(gamePath, query string, manualOnly bool, limit int) []pipeline.CacheEntry {
	return a.session.ListCacheEntries(gamePath, query, manualOnly, limit)
}

// SaveCacheEntry persists a manual translation override.
func (a *App) SaveCacheEntry(gameUID, provider, sourceText, translated string) error {
	return a.session.SaveCacheEntry(gameUID, provider, sourceText, translated)
}

// DeleteCacheEntry removes a translation cache entry.
func (a *App) DeleteCacheEntry(gameUID, provider, sourceText string) error {
	return a.session.DeleteCacheEntry(gameUID, provider, sourceText)
}

// ClearScopeCache clears cached entries for gamePath (or all games when empty).
func (a *App) ClearScopeCache(gamePath string, includeManual bool) int {
	return a.session.ClearScopeCache(gamePath, includeManual)
}

// StartLlamaServer starts the local llama-server.exe process.
func (a *App) StartLlamaServer(cfg config.LlamaCppSettings) (translator.LlamaCppStatus, error) {
	return a.session.StartLlamaServer(cfg)
}

// StopLlamaServer stops the local llama-server.exe process.
func (a *App) StopLlamaServer() translator.LlamaCppStatus {
	return a.session.StopLlamaServer()
}

// LlamaServerStatus returns the current llama-server status.
func (a *App) LlamaServerStatus() translator.LlamaCppStatus {
	return a.session.LlamaServerStatus()
}

// BrowseLlamaBinary opens a file picker for llama-server.exe.
func (a *App) BrowseLlamaBinary() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select llama-server.exe",
		Filters: []runtime.FileFilter{
			{DisplayName: "Executable (*.exe)", Pattern: "*.exe"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// BrowseGGUFModel opens a file picker for .gguf models.
func (a *App) BrowseGGUFModel() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select GGUF Model",
		Filters: []runtime.FileFilter{
			{DisplayName: "GGUF Models (*.gguf)", Pattern: "*.gguf"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
}

// UnityFontStatus returns whether TMP_Font_AssetBundles are installed and any active download progress.
func (a *App) UnityFontStatus() session.UnityFontStatus {
	return a.session.UnityFontStatus()
}

// DownloadUnityFonts downloads and extracts TMP_Font_AssetBundles into %APPDATA%\Khutaf\TMP_Font_AssetBundles.
func (a *App) DownloadUnityFonts() error {
	return a.session.DownloadUnityFontsTo(a.ctx, "appdata")
}

// DownloadUnityFontsTo downloads and extracts TMP_Font_AssetBundles into "appdata" or "app".
func (a *App) DownloadUnityFontsTo(location string) error {
	return a.session.DownloadUnityFontsTo(a.ctx, location)
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
	return a.runTestTranslate(text, a.session.Translate)
}

// TestTranslateWithConfig runs the translation + shaping pipeline using cfg without saving it.
func (a *App) TestTranslateWithConfig(text string, cfg config.TranslatorSettings) (TranslateResult, error) {
	return a.runTestTranslate(text, func(ctx context.Context, s string) (string, bool, error) {
		out, err := a.session.TranslateWithConfig(ctx, cfg, s)
		return out, false, err
	})
}

// BenchmarkTranslators benchmarks multiple providers in parallel and streams per-provider results.
func (a *App) BenchmarkTranslators(req session.BenchmarkRequest) (session.BenchmarkSummary, error) {
	return a.session.BenchmarkTranslators(a.ctx, req)
}

// CancelBenchmark cancels any running translator benchmark.
func (a *App) CancelBenchmark() {
	a.session.CancelBenchmark()
}

// SmartAutoEmbed automatically enables embedding on the best discovered Unity text hooks.
func (a *App) SmartAutoEmbed() (session.SmartAutoEmbedResult, error) {
	return a.session.SmartAutoEmbed()
}

// ApplyHookPreset applies a one-click hook selection preset ("dialogue", "dialogue_ui", "disable_all").
func (a *App) ApplyHookPreset(preset string) (session.HookPresetResult, error) {
	return a.session.ApplyHookPreset(preset)
}

// RestoreEmbedHooks restores a previously snapshotted set of enabled hook keys (undo).
func (a *App) RestoreEmbedHooks(keys []string) (session.HookPresetResult, error) {
	return a.session.RestoreEmbedHooks(keys)
}

// SetHookMuted mutes or unmutes a live hook and persists it in the game profile.
func (a *App) SetHookMuted(key string, muted bool) error {
	return a.session.SetHookMuted(key, muted)
}

// AddGlossaryTerm adds or updates a glossary term for the current game or globally.
func (a *App) AddGlossaryTerm(source, target string, caseSensitive, scopeGame bool) error {
	return a.session.AddGlossaryTerm(source, target, caseSensitive, scopeGame)
}

// CycleDisplayMode cycles between display modes (0..3) and returns the new mode.
func (a *App) CycleDisplayMode() (int, error) {
	mode, err := a.session.CycleDisplayMode()
	return int(mode), err
}

// AdjustFontScale adjusts relative font scale by delta and returns the new scale.
func (a *App) AdjustFontScale(delta float64) (float64, error) {
	scale, err := a.session.AdjustFontScale(delta)
	return float64(scale), err
}

// ExportGameProfile opens a Save dialog and writes a sanitized shareable .khutaf.json profile.
func (a *App) ExportGameProfile(exePath string) (string, error) {
	raw, filename, err := a.session.ExportGameProfileData(exePath, AppVersion)
	if err != nil {
		return "", err
	}
	dst, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Export Game Profile",
		DefaultFilename: filename,
		Filters: []runtime.FileFilter{
			{DisplayName: "Khutaf Game Profile (*.khutaf.json)", Pattern: "*.khutaf.json"},
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || dst == "" {
		return "", err
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

// ImportGameProfile opens a File picker and merges a .khutaf.json profile into targetExePath.
func (a *App) ImportGameProfile(targetExePath string) (session.ImportProfileResult, error) {
	src, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Import Game Profile",
		Filters: []runtime.FileFilter{
			{DisplayName: "Khutaf Game Profile (*.khutaf.json)", Pattern: "*.khutaf.json"},
			{DisplayName: "JSON Files (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil || src == "" {
		return session.ImportProfileResult{}, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return session.ImportProfileResult{}, err
	}
	return a.session.ImportGameProfileData(targetExePath, data)
}

// IsAdmin reports whether Khutaf is currently running with administrator privileges.
func (a *App) IsAdmin() bool {
	return hook.IsAdmin()
}

// RestartAsAdmin launches an elevated Khutaf instance and quits the current process.
func (a *App) RestartAsAdmin() error {
	if err := hook.RestartAsAdmin(); err != nil {
		return err
	}
	if a.ctx != nil {
		go func() {
			time.Sleep(150 * time.Millisecond)
			runtime.Quit(a.ctx)
		}()
	}
	return nil
}

func (a *App) runTestTranslate(text string, fn func(context.Context, string) (string, bool, error)) (TranslateResult, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(a.ctx, 60*time.Second)
	defer cancel()
	trans, cached, err := fn(ctx, text)
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
