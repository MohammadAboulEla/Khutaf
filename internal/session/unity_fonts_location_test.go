//go:build windows

package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnityFontLocationResolutionAndDiscovery(t *testing.T) {
	appDataRoot := t.TempDir()
	runtimeRoot := t.TempDir()

	origOverride := unityFontAppDataOverride
	unityFontAppDataOverride = appDataRoot
	defer func() { unityFontAppDataOverride = origOverride }()

	s := &Session{runtimeDir: runtimeRoot}
	wantAppData := filepath.Join(appDataRoot, "Khutaf", "TMP_Font_AssetBundles")
	wantAppDir := filepath.Join(runtimeRoot, "TMP_Font_AssetBundles")

	if got := s.resolveUnityFontDestDir(""); got != wantAppData {
		t.Fatalf("default dest = %q, want %q", got, wantAppData)
	}
	if got := s.resolveUnityFontDestDir("appdata"); got != wantAppData {
		t.Fatalf("appdata dest = %q, want %q", got, wantAppData)
	}
	if got := s.resolveUnityFontDestDir("app"); got != wantAppDir {
		t.Fatalf("app dest = %q, want %q", got, wantAppDir)
	}

	st := s.UnityFontStatus()
	if st.Installed || st.Location != "" || st.AppDataDir != wantAppData || st.AppDir != wantAppDir {
		t.Fatalf("unexpected initial status: %+v", st)
	}

	if err := os.MkdirAll(wantAppDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wantAppDir, "arialuni_sdf_u2022"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write app bundle: %v", err)
	}
	st = s.UnityFontStatus()
	if !st.Installed || st.Location != "app" || st.Dir != wantAppDir || st.Count != 1 {
		t.Fatalf("expected portable app bundle discovery, got: %+v", st)
	}

	if err := os.MkdirAll(wantAppData, 0o755); err != nil {
		t.Fatalf("mkdir appdata dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wantAppData, "arialuni_sdf_u2022"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write appdata bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wantAppData, "arialuni_sdf_u6000"), []byte("y"), 0o644); err != nil {
		t.Fatalf("write appdata bundle 2: %v", err)
	}
	st = s.UnityFontStatus()
	if !st.Installed || st.Location != "appdata" || st.Dir != wantAppData || st.Count != 2 {
		t.Fatalf("expected appdata bundle discovery, got: %+v", st)
	}
}
