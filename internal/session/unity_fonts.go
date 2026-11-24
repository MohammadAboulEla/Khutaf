//go:build windows

package session

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UnityFontBundleURL is the release asset URL for TMP_Font_AssetBundles.zip.
var UnityFontBundleURL = "https://github.com/MohammadAboulEla/Khutaf/releases/download/unity-fonts/TMP_Font_AssetBundles.zip"

// UnityFontBundleSHA256 is the expected lowercase SHA-256 hex digest of TMP_Font_AssetBundles.zip.
var UnityFontBundleSHA256 = "88eff0018fbc6602343690e975cb833b96463b2655dd14b6a1036fe3d76642c3"

// unityFontAppDataOverride overrides %APPDATA% in unit tests when non-empty.
var unityFontAppDataOverride = ""

// UnityFontStatus describes the installation and download state of Unity TMP font bundles.
type UnityFontStatus struct {
	Installed   bool   `json:"installed"`
	Count       int    `json:"count"`
	Dir         string `json:"dir"`
	Location    string `json:"location"`   // "appdata" | "app" | ""
	AppDataDir  string `json:"appDataDir"` // %APPDATA%\Khutaf\TMP_Font_AssetBundles
	AppDir      string `json:"appDir"`     // <runtimeDir>\TMP_Font_AssetBundles
	Downloading bool   `json:"downloading"`
	Progress    int    `json:"progress"`
	Message     string `json:"message"`
	Error       string `json:"error"`
}

func appDataUnityFontDir() string {
	base := strings.TrimSpace(unityFontAppDataOverride)
	if base == "" {
		if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
			base = dir
		} else {
			base = strings.TrimSpace(os.Getenv("APPDATA"))
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "Khutaf", "TMP_Font_AssetBundles")
}

func (s *Session) appUnityFontDir() string {
	return filepath.Join(s.runtimeDir, "TMP_Font_AssetBundles")
}

func (s *Session) resolveUnityFontDestDir(location string) string {
	loc := strings.ToLower(strings.TrimSpace(location))
	if loc == "app" {
		return s.appUnityFontDir()
	}
	if dir := appDataUnityFontDir(); dir != "" {
		return dir
	}
	return s.appUnityFontDir()
}

func countUnityFontBundles(dir string) int {
	if strings.TrimSpace(dir) == "" {
		return 0
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "arialuni_sdf*"))
	return len(matches)
}

func (s *Session) unityFontMatchesWithLocation() (string, int, string) {
	appDataDir := appDataUnityFontDir()
	appDir := s.appUnityFontDir()
	if n := countUnityFontBundles(appDataDir); n > 0 {
		return appDataDir, n, "appdata"
	}
	if n := countUnityFontBundles(appDir); n > 0 {
		return appDir, n, "app"
	}
	if appDataDir != "" {
		return appDataDir, 0, ""
	}
	return appDir, 0, ""
}

func (s *Session) unityFontMatches() (string, int) {
	dir, count, _ := s.unityFontMatchesWithLocation()
	return dir, count
}

// UnityFontStatus returns whether TMP_Font_AssetBundles are installed and any active download progress.
func (s *Session) UnityFontStatus() UnityFontStatus {
	dir, count, loc := s.unityFontMatchesWithLocation()
	s.unityFontMu.Lock()
	st := s.unityFontState
	s.unityFontMu.Unlock()

	st.Dir = dir
	st.Count = count
	st.Installed = count > 0
	st.Location = loc
	st.AppDataDir = appDataUnityFontDir()
	st.AppDir = s.appUnityFontDir()
	return st
}

func (s *Session) updateUnityFontState(fn func(*UnityFontStatus)) {
	dir, count, loc := s.unityFontMatchesWithLocation()
	s.unityFontMu.Lock()
	fn(&s.unityFontState)
	st := s.unityFontState
	s.unityFontMu.Unlock()

	st.Dir = dir
	st.Count = count
	st.Installed = count > 0
	st.Location = loc
	st.AppDataDir = appDataUnityFontDir()
	st.AppDir = s.appUnityFontDir()
	s.post("unityfonts:progress", st)
}

// DownloadUnityFonts downloads TMP_Font_AssetBundles.zip and extracts it into runtime/TMP_Font_AssetBundles.
func (s *Session) DownloadUnityFonts(ctx context.Context) error {
	return s.DownloadUnityFontsTo(ctx, "app")
}

// DownloadUnityFontsTo downloads TMP_Font_AssetBundles.zip and extracts it into either
// %APPDATA%\Khutaf\TMP_Font_AssetBundles ("appdata", default) or runtime/TMP_Font_AssetBundles ("app").
func (s *Session) DownloadUnityFontsTo(ctx context.Context, location string) error {
	destDir := s.resolveUnityFontDestDir(location)
	s.unityFontMu.Lock()
	if s.unityFontState.Downloading {
		s.unityFontMu.Unlock()
		return errors.New("unity font download already in progress")
	}
	s.unityFontState = UnityFontStatus{
		Downloading: true,
		Progress:    0,
		Message:     "Connecting...",
	}
	s.unityFontMu.Unlock()
	s.updateUnityFontState(func(st *UnityFontStatus) {})

	err := s.runUnityFontDownload(ctx, destDir)
	s.updateUnityFontState(func(st *UnityFontStatus) {
		st.Downloading = false
		if err != nil {
			st.Error = err.Error()
			st.Message = "Download failed"
		} else {
			st.Progress = 100
			st.Error = ""
			st.Message = "Installed"
		}
	})
	if err == nil {
		s.mu.Lock()
		pids := s.pidList()
		s.mu.Unlock()
		for _, pid := range pids {
			s.pushEmbedSettings(pid)
		}
	}
	return err
}

func (s *Session) runUnityFontDownload(ctx context.Context, destDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, UnityFontBundleURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "khutaf-tmp-fonts-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)

	total := resp.ContentLength
	var downloaded int64
	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmpZip.Write(buf[:n]); werr != nil {
				tmpZip.Close()
				return werr
			}
			_, _ = hasher.Write(buf[:n])
			downloaded += int64(n)
			if time.Since(lastEmit) >= 150*time.Millisecond {
				lastEmit = time.Now()
				pct := 0
				msg := fmt.Sprintf("Downloading... %.1f MB", float64(downloaded)/(1024*1024))
				if total > 0 {
					pct = int((downloaded * 90) / total)
					msg = fmt.Sprintf("Downloading... %.1f / %.1f MB", float64(downloaded)/(1024*1024), float64(total)/(1024*1024))
				}
				s.updateUnityFontState(func(st *UnityFontStatus) {
					st.Progress = pct
					st.Message = msg
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			tmpZip.Close()
			return readErr
		}
	}
	if err := tmpZip.Close(); err != nil {
		return err
	}
	if expected := strings.TrimSpace(UnityFontBundleSHA256); expected != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(actual, expected) {
			return fmt.Errorf("unity font bundle SHA-256 mismatch: got %s, expected %s", actual, expected)
		}
	}

	s.updateUnityFontState(func(st *UnityFontStatus) {
		st.Progress = 92
		st.Message = "Extracting bundles..."
	})

	return extractUnityFontZip(tmpZipPath, destDir)
}

func extractUnityFontZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	extracted := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		if !strings.HasPrefix(base, "arialuni_sdf") {
			continue
		}
		if err := extractSingleBundle(f, filepath.Join(destDir, base)); err != nil {
			return err
		}
		extracted++
	}
	if extracted == 0 {
		return errors.New("no arialuni_sdf* files found in archive")
	}
	return nil
}

func extractSingleBundle(f *zip.File, dstPath string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	const maxBundleBytes = 256 << 20
	tmpPath := dstPath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, maxBundleBytes+1))
	if err != nil || n > maxBundleBytes {
		out.Close()
		os.Remove(tmpPath)
		if err != nil {
			return err
		}
		return fmt.Errorf("bundle entry exceeds %d bytes", maxBundleBytes)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dstPath)
}
