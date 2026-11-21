//go:build windows

package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/translator"
)

func TestAutoAttachWatcherAndCooldown(t *testing.T) {
	store := config.NewMemoryStore()
	exe := `C:\Games\AutoGame\AutoGame.exe`
	s := New(store, "", nil)
	defer s.Close()

	if err := s.SetGameAutoAttach(exe, true); err != nil {
		t.Fatalf("SetGameAutoAttach: %v", err)
	}
	if !store.Game(exe).AutoAttach {
		t.Fatal("expected AutoAttach=true in store")
	}

	var attachedPIDs []uint32
	s.runningPathsFn = func(basenames map[string]bool) map[string]uint32 {
		if !basenames["autogame.exe"] {
			return nil
		}
		return map[string]uint32{strings.ToLower(filepath.Clean(exe)): 4242}
	}
	s.autoAttachFn = func(pid uint32) error {
		attachedPIDs = append(attachedPIDs, pid)
		return nil
	}

	// Idle + not on cooldown -> attaches once.
	s.tickAutoAttach()
	if len(attachedPIDs) != 1 || attachedPIDs[0] != 4242 {
		t.Fatalf("attachedPIDs = %v, want [4242]", attachedPIDs)
	}

	// Simulate game currently attached -> watcher stays idle.
	s.mu.Lock()
	s.gamePath = exe
	s.pids[4242] = true
	s.mu.Unlock()
	s.tickAutoAttach()
	if len(attachedPIDs) != 1 {
		t.Fatalf("expected no attach while game already attached, got %v", attachedPIDs)
	}

	// Manual Detach activates 15s cooldown -> immediate tick must skip re-attach.
	s.Detach()
	s.tickAutoAttach()
	if len(attachedPIDs) != 1 {
		t.Fatalf("expected cooldown to skip immediate re-attach, got %v", attachedPIDs)
	}

	// Once cooldown expires, watcher re-attaches cleanly.
	s.autoAttachMu.Lock()
	s.detachCooldown[normalizeExeKey(exe)] = time.Now().Add(-20 * time.Second)
	s.autoAttachMu.Unlock()
	s.tickAutoAttach()
	if len(attachedPIDs) != 2 || attachedPIDs[1] != 4242 {
		t.Fatalf("expected attach after cooldown expired, got %v", attachedPIDs)
	}
}

func TestHotkeyActions(t *testing.T) {
	store := config.NewMemoryStore()
	s := New(store, "", nil)
	defer s.Close()

	// CycleDisplayMode advances global mode wrapping around 0..3.
	startMode := store.Config().Embed.DisplayMode
	for step := 1; step <= 4; step++ {
		want := hook.DisplayMode((startMode + step) % 4)
		got, err := s.CycleDisplayMode()
		if err != nil {
			t.Fatalf("CycleDisplayMode: %v", err)
		}
		if got != want {
			t.Fatalf("CycleDisplayMode step %d = %d, want %d", step, got, want)
		}
	}

	// AdjustFontScale adjusts within [0.5, 2.5] and enables ChangeFontSize.
	scale, err := s.AdjustFontScale(0.1)
	if err != nil {
		t.Fatalf("AdjustFontScale up: %v", err)
	}
	if scale != 1.1 || !store.Config().Embed.ChangeFontSize {
		t.Fatalf("unexpected scale=%v changeFontSize=%v", scale, store.Config().Embed.ChangeFontSize)
	}
	scale, err = s.AdjustFontScale(-2.0)
	if err != nil {
		t.Fatalf("AdjustFontScale clamp low: %v", err)
	}
	if scale != 0.5 {
		t.Fatalf("expected clamp to 0.5, got %v", scale)
	}

	// TogglePause flips Paused and reflects in Status.
	if s.Paused() {
		t.Fatal("expected initial Paused=false")
	}
	if !s.TogglePause() || !s.Status().Paused {
		t.Fatal("expected Paused=true after TogglePause")
	}
	if s.TogglePause() || s.Status().Paused {
		t.Fatal("expected Paused=false after second TogglePause")
	}

	// RetranslateLast without prior line returns an error; with a prior line translates freshly.
	if _, err := s.RetranslateLast(); err == nil {
		t.Fatal("expected error when no line captured yet")
	}

	calls := 0
	s.providerBuilder = func(config.TranslatorSettings) translator.Provider {
		return stubFuncProvider{
			name: "mock",
			fn: func(_ context.Context, text string) (string, error) {
				calls++
				return fmt.Sprintf("ترجمة %d: %s", calls, text), nil
			},
		}
	}
	s.setLastEmbed("Hello World", hook.ThreadParam{ProcessID: 1, Addr: 2})

	line1, err := s.RetranslateLast()
	if err != nil || !strings.Contains(line1.Translated, "ترجمة 1") {
		t.Fatalf("first RetranslateLast = (%+v, %v)", line1, err)
	}
	line2, err := s.RetranslateLast()
	if err != nil || !strings.Contains(line2.Translated, "ترجمة 2") {
		t.Fatalf("second RetranslateLast = (%+v, %v) — expected fresh non-cached result", line2, err)
	}
}

type stubFuncProvider struct {
	name string
	fn   func(context.Context, string) (string, error)
}

func (p stubFuncProvider) Name() string { return p.name }
func (p stubFuncProvider) Translate(ctx context.Context, text string) (string, error) {
	return p.fn(ctx, text)
}
