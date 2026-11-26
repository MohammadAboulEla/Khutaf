//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

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
var AppVersion = "0.2.1"

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

	winMu                        sync.Mutex
	savedW, savedH               int
	savedX, savedY               int
	savedMax, hasSavedGeom       bool
	pendingRestoreGeom           bool
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

func (a *App) updateLoop(ctx context.Context, stop <-chan struct{}) {
	initial := time.NewTimer(5 * time.Second)
	defer initial.Stop()
	select {
	case <-stop:
		return
	case <-ctx.Done():
		return
	case <-initial.C:
	}

	runSilentCheck := func() {
		if a.updater == nil || a.session == nil {
			return
		}
		if !a.session.Config().Update.AutoCheck {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		_, _ = a.updater.Check(cctx, false)
		cancel()
	}

	runSilentCheck()

	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSilentCheck()
		}
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

// HideToTray hides the main window while leaving hooks and tray icon active.
func (a *App) HideToTray() {
	if a.ctx == nil {
		return
	}
	a.windowHidden.Store(true)
	runtime.WindowHide(a.ctx)
}

// ShowFromTray restores and focuses the main window from the system tray.
func (a *App) ShowFromTray() {
	if a.ctx == nil {
		return
	}
	if a.hooksStandalone.Load() {
		a.closeStandaloneHooksPopup(false)
	}
	a.windowHidden.Store(false)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	a.EnsureMainWindowGeometry()
}

// EnsureMainWindowGeometry restores saved main window bounds after a deferred standalone close.
func (a *App) EnsureMainWindowGeometry() {
	if a.ctx == nil || a.hooksStandalone.Load() {
		return
	}
	a.winMu.Lock()
	needRestore := a.pendingRestoreGeom
	a.pendingRestoreGeom = false
	a.winMu.Unlock()
	if needRestore {
		a.restoreMainWindowGeometry()
	}
}

// ToggleWindowVisibility alternates between showing and hiding the main window.
func (a *App) ToggleWindowVisibility() {
	if a.windowHidden.Load() {
		a.ShowFromTray()
		return
	}
	a.HideToTray()
}

// SetHooksPopupOpen synchronizes the Quick Hooks modal state from the frontend.
func (a *App) SetHooksPopupOpen(open bool) {
	if !open && a.hooksStandalone.Load() {
		a.closeStandaloneHooksPopup(true)
		return
	}
	a.hooksPopupOpen.Store(open)
	if !open {
		a.openedByHotkey.Store(false)
	}
}

// ShowMainFromHooksPopup closes the Quick Hooks popup and restores the full Khutaf window.
func (a *App) ShowMainFromHooksPopup() {
	if a.hooksStandalone.Load() {
		a.closeStandaloneHooksPopup(false)
		if a.ctx != nil {
			a.windowHidden.Store(false)
			runtime.WindowUnminimise(a.ctx)
			if !runtime.WindowIsMaximised(a.ctx) {
				runtime.WindowCenter(a.ctx)
			}
			runtime.WindowShow(a.ctx)
		}
		return
	}
	a.SetHooksPopupOpen(false)
}

func (a *App) saveStandaloneHooksGeometry() {
	if a.ctx == nil || a.session == nil {
		return
	}
	pw, ph := runtime.WindowGetSize(a.ctx)
	px, py := runtime.WindowGetPosition(a.ctx)
	if pw < 320 || ph < 240 {
		return
	}
	cfg := a.session.Config()
	hk := cfg.Hotkeys
	if hk.PopupW == pw && hk.PopupH == ph && hk.PopupX == px && hk.PopupY == py && hk.PopupHasPos {
		return
	}
	cfg.Hotkeys.PopupW = pw
	cfg.Hotkeys.PopupH = ph
	cfg.Hotkeys.PopupX = px
	cfg.Hotkeys.PopupY = py
	cfg.Hotkeys.PopupHasPos = true
	_ = a.session.SaveConfig(cfg)
}

func (a *App) openStandaloneHooksPopup() {
	if a.ctx == nil {
		return
	}
	var wasMax bool
	a.winMu.Lock()
	if !a.hooksStandalone.Load() {
		wasMax = runtime.WindowIsMaximised(a.ctx)
		if !a.pendingRestoreGeom {
			w, h := runtime.WindowGetSize(a.ctx)
			x, y := runtime.WindowGetPosition(a.ctx)
			if w >= 860 && h >= 560 {
				a.savedW, a.savedH = w, h
				a.savedX, a.savedY = x, y
				a.savedMax = wasMax
				a.hasSavedGeom = true
			} else {
				a.savedW, a.savedH = 1120, 760
				a.savedMax = false
				a.hasSavedGeom = false
			}
		} else if a.savedMax {
			wasMax = true
		}
	} else {
		wasMax = runtime.WindowIsMaximised(a.ctx)
	}
	a.winMu.Unlock()

	a.hooksPopupOpen.Store(true)
	a.hooksStandalone.Store(true)
	a.openedByHotkey.Store(true)

	popupW, popupH := 600, 480
	var hasPos bool
	var popupX, popupY int
	if a.session != nil {
		hk := a.session.Config().Hotkeys
		if hk.PopupW >= 320 && hk.PopupH >= 240 {
			popupW, popupH = hk.PopupW, hk.PopupH
		}
		if hk.PopupHasPos {
			hasPos = true
			popupX, popupY = hk.PopupX, hk.PopupY
		}
	}

	// Switch the Vue view to the standalone picker before displaying the window.
	runtime.EventsEmit(a.ctx, "hooks:popup", map[string]any{"open": true, "standalone": true})
	time.Sleep(25 * time.Millisecond)

	// Restore and show before sizing/positioning so Win32 SW_RESTORE cannot override saved bounds.
	runtime.WindowSetMinSize(a.ctx, 320, 240)
	a.windowHidden.Store(false)
	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	if wasMax {
		runtime.WindowUnmaximise(a.ctx)
	}
	runtime.WindowSetSize(a.ctx, popupW, popupH)
	if hasPos {
		runtime.WindowSetPosition(a.ctx, popupX, popupY)
	} else {
		runtime.WindowCenter(a.ctx)
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
}

func (a *App) restoreMainWindowGeometry() {
	if a.ctx == nil {
		return
	}
	a.winMu.Lock()
	w, h := a.savedW, a.savedH
	x, y := a.savedX, a.savedY
	wasMax := a.savedMax
	hasPos := a.hasSavedGeom
	a.winMu.Unlock()

	if w < 860 || h < 560 {
		w, h = 1120, 760
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.WindowSetMinSize(a.ctx, 860, 560)
	runtime.WindowSetSize(a.ctx, w, h)
	if hasPos {
		runtime.WindowSetPosition(a.ctx, x, y)
	} else {
		runtime.WindowCenter(a.ctx)
	}
	if wasMax {
		runtime.WindowMaximise(a.ctx)
	}
}

func (a *App) closeStandaloneHooksPopup(hideWindow bool) {
	a.hooksPopupOpen.Store(false)
	a.openedByHotkey.Store(false)
	wasStandalone := a.hooksStandalone.Swap(false)
	if a.ctx == nil {
		return
	}
	if wasStandalone {
		a.saveStandaloneHooksGeometry()
	}
	if hideWindow {
		// Defer main min-size/geometry restore until shown again so Win32 doesn't clamp the popup to 860x560.
		runtime.WindowSetAlwaysOnTop(a.ctx, false)
		if wasStandalone {
			a.winMu.Lock()
			a.pendingRestoreGeom = true
			a.winMu.Unlock()
		}
		if a.session != nil && a.session.Config().Hotkeys.MinimizeToTray {
			a.HideToTray()
		} else {
			runtime.WindowMinimise(a.ctx)
		}
	} else if wasStandalone {
		a.winMu.Lock()
		a.pendingRestoreGeom = false
		a.winMu.Unlock()
		a.restoreMainWindowGeometry()
	}
	runtime.EventsEmit(a.ctx, "hooks:popup", map[string]any{"open": false, "standalone": false})
}

// QuitApp exits Khutaf cleanly, bypassing MinimizeToTray close interception.
func (a *App) QuitApp() {
	a.quitting.Store(true)
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) trayState() TrayState {
	if a.session == nil {
		return TrayState{}
	}
	st := a.session.Status()
	cfg := a.session.Config()
	return TrayState{
		Attached: st.Attached,
		GameName: st.GameName,
		Paused:   st.Paused,
		Lang:     cfg.UI.Language,
		Hidden:   a.windowHidden.Load(),
	}
}

func (a *App) applyHotkeys(hk config.HotkeySettings) {
	if a.hotkeys == nil {
		return
	}
	if err := a.hotkeys.Configure(hotkeys.BindingsFromConfig(hk)); err != nil {
		if a.session != nil {
			a.session.EmitHotkeyToast("warning", err.Error())
		}
		if a.ctx != nil {
			runtime.LogWarning(a.ctx, err.Error())
		}
	}
}

func (a *App) handleHotkey(act hotkeys.Action) {
	if a.session == nil {
		return
	}
	ar := a.session.Config().UI.Language != "en"
	switch act {
	case hotkeys.ActionCycleMode:
		mode, err := a.session.CycleDisplayMode()
		if err != nil {
			a.session.EmitHotkeyToast(string(act), err.Error())
			return
		}
		namesEN := []string{"Translation Only", "Original + Translation", "Translation + Original", "Original Only"}
		namesAR := []string{"الترجمة فقط", "الأصلي + الترجمة", "الترجمة + الأصلي", "الأصلي فقط"}
		idx := int(mode)
		if idx < 0 || idx >= len(namesEN) {
			idx = 0
		}
		if ar {
			a.session.EmitHotkeyToast(string(act), "نمط العرض: "+namesAR[idx])
		} else {
			a.session.EmitHotkeyToast(string(act), "Display Mode: "+namesEN[idx])
		}
	case hotkeys.ActionTogglePause:
		paused := a.session.TogglePause()
		if ar {
			if paused {
				a.session.EmitHotkeyToast(string(act), "تم إيقاف الترجمة مؤقتاً")
			} else {
				a.session.EmitHotkeyToast(string(act), "تم استئناف الترجمة")
			}
		} else {
			if paused {
				a.session.EmitHotkeyToast(string(act), "Translation Paused")
			} else {
				a.session.EmitHotkeyToast(string(act), "Translation Resumed")
			}
		}
	case hotkeys.ActionRetranslate:
		if _, err := a.session.RetranslateLast(); err != nil {
			a.session.EmitHotkeyToast(string(act), err.Error())
			return
		}
		if ar {
			a.session.EmitHotkeyToast(string(act), "تمت إعادة ترجمة آخر سطر")
		} else {
			a.session.EmitHotkeyToast(string(act), "Re-translated last line")
		}
	case hotkeys.ActionFontScaleUp, hotkeys.ActionFontScaleDown:
		delta := 0.1
		if act == hotkeys.ActionFontScaleDown {
			delta = -0.1
		}
		scale, err := a.session.AdjustFontScale(delta)
		if err != nil {
			a.session.EmitHotkeyToast(string(act), err.Error())
			return
		}
		if ar {
			a.session.EmitHotkeyToast(string(act), fmt.Sprintf("حجم الخط: %.1fx", scale))
		} else {
			a.session.EmitHotkeyToast(string(act), fmt.Sprintf("Font Scale: %.1fx", scale))
		}
	case hotkeys.ActionToggleHooksMenu:
		if a.hooksPopupOpen.Load() {
			a.closeStandaloneHooksPopup(a.openedByHotkey.Load() || a.hooksStandalone.Load())
			return
		}
		a.openStandaloneHooksPopup()
	}
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

// GetUpdateStatus returns the current auto-update status snapshot.
func (a *App) GetUpdateStatus() updater.Status {
	if a.updater == nil {
		return updater.Status{State: updater.StateIdle, CurrentVersion: AppVersion, ReleaseURL: updater.DefaultReleasesPageURL}
	}
	return a.updater.Status()
}

// CheckForUpdates checks GitHub Releases for a newer version.
func (a *App) CheckForUpdates(manual bool) (updater.Status, error) {
	if a.updater == nil {
		return a.GetUpdateStatus(), errors.New("updater not initialized")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return a.updater.Check(ctx, manual)
}

// DownloadUpdate downloads, verifies, and stages the available update release.
func (a *App) DownloadUpdate() error {
	if a.updater == nil {
		return errors.New("updater not initialized")
	}
	return a.updater.Download(a.ctx)
}

// CancelUpdateDownload cancels an in-progress update download.
func (a *App) CancelUpdateDownload() {
	if a.updater != nil {
		a.updater.CancelDownload()
	}
}

// InstallUpdate launches the isolated self-updater and exits Khutaf cleanly to swap binaries.
func (a *App) InstallUpdate(detachIfAttached bool) error {
	if a.updater == nil {
		return errors.New("updater not initialized")
	}
	if a.session != nil && a.session.Status().Attached {
		if !detachIfAttached {
			return errors.New("game_attached")
		}
		a.session.Detach()
	}
	helperExe, args, err := a.updater.PrepareApply()
	if err != nil {
		return err
	}
	if err := updater.LaunchHelper(helperExe, args); err != nil {
		return err
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		a.QuitApp()
	}()
	return nil
}

// SkipUpdateVersion persists a skipped version so silent checks do not re-notify for it.
func (a *App) SkipUpdateVersion(version string) error {
	a.updateMu.Lock()
	cfg := a.session.Config()
	cfg.Update.SkippedVersion = strings.TrimPrefix(strings.TrimSpace(version), "v")
	err := a.session.SaveConfig(cfg)
	a.updateMu.Unlock()
	if err == nil && a.updater != nil {
		a.updater.MarkSkipped(version)
	}
	return err
}

// OpenReleasePage opens the GitHub release page in the user's default browser.
func (a *App) OpenReleasePage(rawURL string) {
	target := updater.DefaultReleasesPageURL
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmed, "https://github.com/MohammadAboulEla/Khutaf/releases") {
		target = trimmed
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, target)
	}
}

var (
	user32DLL               = syscall.NewLazyDLL("user32.dll")
	gdi32DLL                = syscall.NewLazyDLL("gdi32.dll")
	procGetDC               = user32DLL.NewProc("GetDC")
	procReleaseDC           = user32DLL.NewProc("ReleaseDC")
	procEnumFontFamiliesW   = gdi32DLL.NewProc("EnumFontFamiliesExW")
	procCreateFontIndirectW = gdi32DLL.NewProc("CreateFontIndirectW")
	procSelectObject        = gdi32DLL.NewProc("SelectObject")
	procDeleteObject        = gdi32DLL.NewProc("DeleteObject")
	procGetGlyphIndicesW    = gdi32DLL.NewProc("GetGlyphIndicesW")
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

var arabicShapedProbe = func() []uint16 {
	out := make([]uint16, 0, 0xFEFC-0xFE80+1)
	for cp := uint16(0xFE80); cp <= 0xFEFC; cp++ {
		out = append(out, cp)
	}
	return out
}()

var (
	fontEnumMu       sync.Mutex
	fontEnumSeen     map[string]bool
	fontListCache    []string
	allFontListCache []string
	enumFontProc     = syscall.NewCallback(func(lf *logFontW, _ uintptr, _ uintptr, _ uintptr) uintptr {
		name := windows.UTF16ToString(lf.FaceName[:])
		if name != "" && !strings.HasPrefix(name, "@") {
			fontEnumSeen[name] = true
		}
		return 1
	})
)

func fontSupportsArabicShaped(hdc uintptr, family string) bool {
	var lf logFontW
	lf.CharSet = 1 // DEFAULT_CHARSET
	encoded := utf16.Encode([]rune(family))
	if len(encoded) > len(lf.FaceName)-1 {
		encoded = encoded[:len(lf.FaceName)-1]
	}
	copy(lf.FaceName[:], encoded)

	hfont, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(&lf)))
	if hfont == 0 {
		return false
	}
	defer procDeleteObject.Call(hfont)

	old, _, _ := procSelectObject.Call(hdc, hfont)
	if old == 0 {
		return false
	}
	defer procSelectObject.Call(hdc, old)

	var indices [0xFEFC - 0xFE80 + 1]uint16
	const ggiMarkNonexistingGlyphs = 0x0001
	ret, _, _ := procGetGlyphIndicesW.Call(
		hdc,
		uintptr(unsafe.Pointer(&arabicShapedProbe[0])),
		uintptr(len(arabicShapedProbe)),
		uintptr(unsafe.Pointer(&indices[0])),
		ggiMarkNonexistingGlyphs,
	)
	if uint32(ret) == 0xFFFFFFFF {
		return false
	}
	missing := 0
	for _, idx := range indices {
		if idx == 0xFFFF {
			missing++
			if missing > 2 {
				return false
			}
		}
	}
	return true
}

func listSystemFonts(filterArabic bool) []string {
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return []string{"Arial", "Segoe UI", "Tahoma"}
	}
	defer procReleaseDC.Call(0, hdc)

	fontEnumSeen = map[string]bool{}
	lf := logFontW{CharSet: 1} // DEFAULT_CHARSET
	procEnumFontFamiliesW.Call(hdc, uintptr(unsafe.Pointer(&lf)), enumFontProc, 0, 0)
	out := make([]string, 0, len(fontEnumSeen))
	for n := range fontEnumSeen {
		if !filterArabic || fontSupportsArabicShaped(hdc, n) {
			out = append(out, n)
		}
	}
	fontEnumSeen = nil

	if len(out) == 0 {
		return []string{"Arial", "Segoe UI", "Tahoma"}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

// ListFonts returns installed Windows font family names with complete Arabic glyph support.
func (a *App) ListFonts() []string {
	fontEnumMu.Lock()
	defer fontEnumMu.Unlock()

	if len(fontListCache) > 0 {
		cp := make([]string, len(fontListCache))
		copy(cp, fontListCache)
		return cp
	}

	out := listSystemFonts(true)
	fontListCache = make([]string, len(out))
	copy(fontListCache, out)
	return out
}

// ListAllFonts returns all installed Windows font family names without Arabic glyph filtering.
func (a *App) ListAllFonts() []string {
	fontEnumMu.Lock()
	defer fontEnumMu.Unlock()

	if len(allFontListCache) > 0 {
		cp := make([]string, len(allFontListCache))
		copy(cp, allFontListCache)
		return cp
	}

	out := listSystemFonts(false)
	allFontListCache = make([]string, len(out))
	copy(allFontListCache, out)
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
