//go:build windows

package hotkeys

import (
	"errors"
	"strings"
	"testing"

	"khutaf/internal/config"
)

func TestParseCombo(t *testing.T) {
	valid := []struct {
		combo string
		mods  uint32
		vk    uint32
	}{
		{"Ctrl+Alt+M", modControl | modAlt, 'M'},
		{"ctrl + shift + f10", modControl | modShift, 0x79},
		{"Ctrl+Alt+Up", modControl | modAlt, 0x26},
		{"Ctrl+Alt+Down", modControl | modAlt, 0x28},
		{"F12", 0, 0x7B},
		{"Win+Shift+1", modWin | modShift, '1'},
		{"Ctrl+Alt+=", modControl | modAlt, 0xBB},
	}
	for _, tc := range valid {
		mods, vk, ok := ParseCombo(tc.combo)
		if !ok {
			t.Fatalf("ParseCombo(%q) expected ok=true", tc.combo)
		}
		if mods != tc.mods || vk != tc.vk {
			t.Fatalf("ParseCombo(%q) = (0x%x, 0x%x), want (0x%x, 0x%x)", tc.combo, mods, vk, tc.mods, tc.vk)
		}
	}

	invalid := []string{"", "   ", "A", "Ctrl+Alt", "Ctrl++M", "Ctrl+UnknownKey", "Ctrl+A+B"}
	for _, combo := range invalid {
		if _, _, ok := ParseCombo(combo); ok {
			t.Fatalf("ParseCombo(%q) expected ok=false", combo)
		}
	}
}

func TestBindingsFromConfig(t *testing.T) {
	cfg := config.Default().Hotkeys
	got := BindingsFromConfig(cfg)
	if len(got) != 6 {
		t.Fatalf("expected 6 active bindings, got %d (%+v)", len(got), got)
	}
	if got[ActionToggleHooksMenu] != "Ctrl+Alt+H" {
		t.Fatalf("ToggleHooksMenu = %q, want Ctrl+Alt+H", got[ActionToggleHooksMenu])
	}

	cfg.TogglePause = ""
	got = BindingsFromConfig(cfg)
	if _, exists := got[ActionTogglePause]; exists {
		t.Fatal("expected empty combo to omit TogglePause binding")
	}

	cfg.Enabled = false
	if disabled := BindingsFromConfig(cfg); len(disabled) != 0 {
		t.Fatalf("expected empty map when disabled, got %+v", disabled)
	}
}

func TestManagerLifecycleAndConflicts(t *testing.T) {
	registered := map[int]uint32{}
	var unregCalls int
	m := NewManager(nil)
	m.registerFn = func(id int, mods, vk uint32) error {
		if mods == (modControl|modAlt) && vk == 'P' {
			return errors.New("hotkey already registered")
		}
		registered[id] = vk
		return nil
	}
	m.unregisterFn = func(id int) {
		delete(registered, id)
		unregCalls++
	}
	defer m.Close()

	// Invalid combo + conflicted combo report warnings while clean combos register.
	err := m.Configure(map[Action]string{
		ActionCycleMode:   "BadOnlyKey",
		ActionTogglePause: "Ctrl+Alt+P",
		ActionRetranslate: "Ctrl+Alt+R",
	})
	if err == nil || !strings.Contains(err.Error(), "cycleMode") || !strings.Contains(err.Error(), "togglePause") {
		t.Fatalf("expected invalid + conflict warnings, got: %v", err)
	}
	if len(registered) != 1 {
		t.Fatalf("expected clean binding to still register, got %v", registered)
	}

	// Valid bindings succeed cleanly with zero warnings and unregister previous IDs first.
	if err := m.Configure(map[Action]string{
		ActionCycleMode:       "Ctrl+Alt+M",
		ActionToggleHooksMenu: "Ctrl+Alt+H",
	}); err != nil {
		t.Fatalf("expected clean reconfigure, got: %v", err)
	}
	if len(registered) != 2 || unregCalls == 0 {
		t.Fatalf("registered=%v unregCalls=%d", registered, unregCalls)
	}

	// Clearing all bindings unregisters cleanly.
	if err := m.Configure(map[Action]string{}); err != nil {
		t.Fatalf("clear configure: %v", err)
	}
	if len(registered) != 0 {
		t.Fatalf("expected all bindings unregistered, got %v", registered)
	}
}
