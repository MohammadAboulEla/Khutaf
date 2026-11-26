//go:build windows

package updater

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"khutaf/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body any, headers map[string]string) *http.Response {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(&buf),
	}
}

func bytesResponse(status int, raw []byte) *http.Response {
	return &http.Response{
		StatusCode:    status,
		ContentLength: int64(len(raw)),
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(raw)),
	}
}

func TestSemverParseAndCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.5-beta", "0.1.5", -1},
		{"v0.1.5-beta", "0.1.5-beta", 0},
		{"0.1.5-beta", "0.1.6-beta", -1},
		{"0.1.6-alpha", "0.1.6-beta", -1},
		{"0.2.0", "0.1.9", 1},
		{"v0.2", "0.2.0", 0},
		{"0.1.5-beta.2", "0.1.5-beta.10", -1},
	}
	for _, tc := range cases {
		va, err := ParseVersion(tc.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.a, err)
		}
		vb, err := ParseVersion(tc.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", tc.b, err)
		}
		if got := va.Compare(vb); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}

	if got := ExtractAssetVersion("Khutaf-v0.1.5-beta-win64.zip"); got != "0.1.5-beta" {
		t.Fatalf("ExtractAssetVersion = %q, want 0.1.5-beta", got)
	}
	if got := ExtractAssetVersion("Khutaf-v0.2.0-win64.zip"); got != "0.2.0" {
		t.Fatalf("ExtractAssetVersion = %q, want 0.2.0", got)
	}
	if got := ExtractAssetVersion("TMP_Font_AssetBundles.zip"); got != "" {
		t.Fatalf("ExtractAssetVersion non-app = %q, want empty", got)
	}
}

func makeTestZip(t *testing.T, files map[string]string) ([]byte, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func TestManagerCheckFiltersAndETag(t *testing.T) {
	oldAPI := DefaultReleasesAPIURL
	defer func() { DefaultReleasesAPIURL = oldAPI }()
	DefaultReleasesAPIURL = "https://api.github.com/repos/MohammadAboulEla/Khutaf/releases?per_page=15"

	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("If-None-Match") == `"etag-1"` {
			return bytesResponse(http.StatusNotModified, nil), nil
		}
		releases := []ghRelease{
			{
				TagName: "unity-fonts",
				Name:    "Unity Fonts",
				Assets:  []ghAsset{{Name: "TMP_Font_AssetBundles.zip", BrowserDownloadURL: "https://github.com/MohammadAboulEla/Khutaf/releases/download/unity-fonts/TMP_Font_AssetBundles.zip"}},
			},
			{
				TagName: "v0.9.0",
				Draft:   true,
				Assets:  []ghAsset{{Name: "Khutaf-v0.9.0-win64.zip", BrowserDownloadURL: "https://github.com/MohammadAboulEla/Khutaf/releases/download/v0.9.0/Khutaf-v0.9.0-win64.zip"}},
			},
			{
				TagName:     "v0.2.0-beta",
				Name:        "Khutaf v0.2.0-beta",
				Body:        "- New feature",
				Prerelease:  true,
				PublishedAt: "2026-11-25T12:00:00Z",
				Assets: []ghAsset{{
					Name:               "Khutaf-v0.2.0-beta-win64.zip",
					Size:               1024,
					Digest:             "sha256:" + strings.Repeat("a", 64),
					BrowserDownloadURL: "https://github.com/MohammadAboulEla/Khutaf/releases/download/v0.2.0-beta/Khutaf-v0.2.0-beta-win64.zip",
				}},
			},
			{
				TagName:    "v0.1.5",
				Prerelease: true,
				Assets:     []ghAsset{{Name: "Khutaf-v0.1.5-beta-win64.zip", BrowserDownloadURL: "https://github.com/MohammadAboulEla/Khutaf/releases/download/v0.1.5/Khutaf-v0.1.5-beta-win64.zip"}},
			},
		}
		return jsonResponse(http.StatusOK, releases, map[string]string{"ETag": `"etag-1"`}), nil
	})

	cfg := config.UpdateSettings{AutoCheck: true, IncludePrerelease: true}
	mgr := New("0.1.5-beta", t.TempDir(), nil, func() config.UpdateSettings { return cfg }, func(last, etag string) {
		cfg.LastCheckedAt = last
		cfg.ETag = etag
	})
	mgr.httpClient.Transport = rt

	st, err := mgr.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if st.State != StateAvailable || st.LatestVersion != "0.2.0-beta" {
		t.Fatalf("got state=%s latest=%s, want available/0.2.0-beta", st.State, st.LatestVersion)
	}
	if cfg.ETag != `"etag-1"` {
		t.Fatalf("saved ETag = %q, want %q", cfg.ETag, `"etag-1"`)
	}

	// Second silent check should hit 304 Not Modified and preserve available state.
	st2, err := mgr.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check 304: %v", err)
	}
	if st2.State != StateAvailable || st2.LatestVersion != "0.2.0-beta" {
		t.Fatalf("after 304 got state=%s latest=%s, want available/0.2.0-beta", st2.State, st2.LatestVersion)
	}

	// Skipping the version suppresses silent notification, but manual check still surfaces it.
	cfg.SkippedVersion = "0.2.0-beta"
	cfg.ETag = ""
	stSilent, err := mgr.Check(context.Background(), false)
	if err != nil {
		t.Fatalf("Check skipped silent: %v", err)
	}
	if stSilent.State != StateUpToDate {
		t.Fatalf("skipped silent state = %s, want up_to_date", stSilent.State)
	}
	stManual, err := mgr.Check(context.Background(), true)
	if err != nil {
		t.Fatalf("Check skipped manual: %v", err)
	}
	if stManual.State != StateAvailable {
		t.Fatalf("skipped manual state = %s, want available", stManual.State)
	}
}

func TestManagerDownloadVerifyAndZipSlip(t *testing.T) {
	oldAPI := DefaultReleasesAPIURL
	defer func() { DefaultReleasesAPIURL = oldAPI }()
	DefaultReleasesAPIURL = "https://api.github.com/repos/MohammadAboulEla/Khutaf/releases?per_page=15"

	validZip, validSHA := makeTestZip(t, map[string]string{
		"Khutaf.exe":                "new-exe",
		"runtime/khutaf-hook64.dll": "new-dll",
		"userconfig/config.json":    "should-be-ignored",
	})
	badZip, badZipSHA := makeTestZip(t, map[string]string{
		"../../evil.txt":            "pwn",
		"Khutaf.exe":                "exe",
		"runtime/khutaf-hook64.dll": "dll",
	})

	serveZip := validZip
	serveSHA := validSHA
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/repos/MohammadAboulEla/Khutaf/releases":
			return jsonResponse(http.StatusOK, []ghRelease{{
				TagName: "v0.2.0",
				Assets: []ghAsset{
					{
						Name:               "Khutaf-v0.2.0-win64.zip",
						Size:               int64(len(serveZip)),
						Digest:             "sha256:" + serveSHA,
						BrowserDownloadURL: "https://github.com/MohammadAboulEla/Khutaf/releases/download/v0.2.0/Khutaf-v0.2.0-win64.zip",
					},
				},
			}}, nil), nil
		case "/MohammadAboulEla/Khutaf/releases/download/v0.2.0/Khutaf-v0.2.0-win64.zip":
			return bytesResponse(http.StatusOK, serveZip), nil
		default:
			return bytesResponse(http.StatusNotFound, nil), nil
		}
	})

	installDir := t.TempDir()
	mgr := New("0.1.5-beta", installDir, nil, nil, nil)
	mgr.httpClient.Transport = rt
	if _, err := mgr.Check(context.Background(), true); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if err := mgr.Download(context.Background()); err != nil {
		t.Fatalf("Download valid: %v", err)
	}
	st := mgr.Status()
	if st.State != StateReady {
		t.Fatalf("state = %s, want ready", st.State)
	}
	if _, err := os.Stat(filepath.Join(installDir, stageDirName, "userconfig", "config.json")); !os.IsNotExist(err) {
		t.Fatal("userconfig must never be staged from update archive")
	}

	// Mismatched SHA-256 must fail.
	serveSHA = strings.Repeat("b", 64)
	mgrBadHash := New("0.1.5-beta", t.TempDir(), nil, nil, nil)
	mgrBadHash.httpClient.Transport = rt
	if _, err := mgrBadHash.Check(context.Background(), true); err != nil {
		t.Fatalf("Check badHash: %v", err)
	}
	if err := mgrBadHash.Download(context.Background()); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected SHA-256 mismatch error, got: %v", err)
	}

	// Zip-Slip path must be rejected.
	serveZip = badZip
	serveSHA = badZipSHA
	mgrSlip := New("0.1.5-beta", t.TempDir(), nil, nil, nil)
	mgrSlip.httpClient.Transport = rt
	if _, err := mgrSlip.Check(context.Background(), true); err != nil {
		t.Fatalf("Check zipSlip: %v", err)
	}
	if err := mgrSlip.Download(context.Background()); err == nil || !strings.Contains(err.Error(), "unsafe path") {
		t.Fatalf("expected unsafe path error, got: %v", err)
	}
}

func TestApplyUpdateSwapAndCleanup(t *testing.T) {
	targetDir := t.TempDir()
	stageDir := filepath.Join(targetDir, stageDirName)
	if err := os.MkdirAll(filepath.Join(targetDir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(targetDir, "userconfig"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stageDir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}

	_ = os.WriteFile(filepath.Join(targetDir, "Khutaf.exe"), []byte("old-exe"), 0o644)
	_ = os.WriteFile(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll"), []byte("old-dll"), 0o644)
	_ = os.MkdirAll(filepath.Join(targetDir, "runtime", "TMP_Font_AssetBundles"), 0o755)
	_ = os.WriteFile(filepath.Join(targetDir, "runtime", "TMP_Font_AssetBundles", "arialuni_sdf_u2019"), []byte("font-bundle"), 0o644)
	_ = os.WriteFile(filepath.Join(targetDir, "userconfig", "config.json"), []byte(`{"keep":true}`), 0o644)
	_ = os.WriteFile(filepath.Join(stageDir, "Khutaf.exe"), []byte("new-exe"), 0o644)
	_ = os.WriteFile(filepath.Join(stageDir, "runtime", "khutaf-hook64.dll"), []byte("new-dll"), 0o644)

	if err := ApplyUpdate(0, targetDir, stageDir, "0.1.5-beta", false); err != nil {
		t.Fatalf("ApplyUpdate: %v", err)
	}

	gotExe, _ := os.ReadFile(filepath.Join(targetDir, "Khutaf.exe"))
	gotDLL, _ := os.ReadFile(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll"))
	gotFont, _ := os.ReadFile(filepath.Join(targetDir, "runtime", "TMP_Font_AssetBundles", "arialuni_sdf_u2019"))
	gotCfg, _ := os.ReadFile(filepath.Join(targetDir, "userconfig", "config.json"))
	if string(gotExe) != "new-exe" || string(gotDLL) != "new-dll" {
		t.Fatalf("unexpected swapped files: exe=%q dll=%q", gotExe, gotDLL)
	}
	if string(gotFont) != "font-bundle" {
		t.Fatalf("TMP_Font_AssetBundles was altered: %q", gotFont)
	}
	if string(gotCfg) != `{"keep":true}` {
		t.Fatalf("userconfig was altered: %q", gotCfg)
	}

	CleanupOldFiles(targetDir)
	if _, err := os.Stat(filepath.Join(targetDir, "Khutaf.exe.old")); !os.IsNotExist(err) {
		t.Fatal("expected Khutaf.exe.old to be removed by CleanupOldFiles")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll.old")); !os.IsNotExist(err) {
		t.Fatal("expected khutaf-hook64.dll.old to be removed by CleanupOldFiles")
	}
}

func TestApplyUpdateRollbackOnFailure(t *testing.T) {
	targetDir := t.TempDir()
	stageDir := filepath.Join(targetDir, stageDirName)
	if err := os.MkdirAll(filepath.Join(stageDir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(targetDir, "Khutaf.exe"), []byte("orig-exe"), 0o644)
	_ = os.WriteFile(filepath.Join(stageDir, "Khutaf.exe"), []byte("new-exe"), 0o644)
	// Place a non-empty directory at runtime/khutaf-hook64.dll.old so backing up the existing DLL fails mid-swap.
	if err := os.MkdirAll(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll.old"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll"), []byte("orig-dll"), 0o644)
	_ = os.WriteFile(filepath.Join(targetDir, "runtime", "khutaf-hook64.dll.old", "blocker"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(stageDir, "runtime", "khutaf-hook64.dll"), []byte("new-dll"), 0o644)

	err := ApplyUpdate(0, targetDir, stageDir, "0.1.5-beta", false)
	if err == nil {
		t.Fatal("expected ApplyUpdate to fail when backup path is blocked by non-empty directory")
	}
	gotExe, _ := os.ReadFile(filepath.Join(targetDir, "Khutaf.exe"))
	if string(gotExe) != "orig-exe" {
		t.Fatalf("expected rollback of Khutaf.exe to orig-exe, got %q", gotExe)
	}
	if msg := ReadAndClearUpdateError(targetDir); msg == "" {
		t.Fatal("expected rollback error message file to be written")
	}
}
