//go:build windows

package main

import (
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

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
