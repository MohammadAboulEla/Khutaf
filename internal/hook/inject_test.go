//go:build windows

package hook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectAntiCheatByGameDirectory(t *testing.T) {
	cleanDir := t.TempDir()
	cleanExe := filepath.Join(cleanDir, "Game.exe")
	if got := DetectAntiCheat(nil, cleanExe); got != "" {
		t.Fatalf("clean game dir returned %q, want empty", got)
	}

	eacDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(eacDir, "EasyAntiCheat"), 0o755); err != nil {
		t.Fatalf("mkdir EasyAntiCheat: %v", err)
	}
	if got := DetectAntiCheat(nil, filepath.Join(eacDir, "Game.exe")); got != "EasyAntiCheat" {
		t.Fatalf("EAC dir returned %q, want EasyAntiCheat", got)
	}

	beDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(beDir, "BattlEye"), 0o755); err != nil {
		t.Fatalf("mkdir BattlEye: %v", err)
	}
	if got := DetectAntiCheat(nil, filepath.Join(beDir, "Game.exe")); got != "BattlEye" {
		t.Fatalf("BattlEye dir returned %q, want BattlEye", got)
	}
}
