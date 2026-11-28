//go:build windows

package main

import (
	"fmt"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"khutaf/internal/config"
	"khutaf/internal/hotkeys"
)

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
