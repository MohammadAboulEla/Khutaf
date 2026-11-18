package session

import (
	"archive/zip"
	"context"
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

// UnityFontStatus describes the installation and download state of Unity TMP font bundles.
type UnityFontStatus struct {
	Installed   bool   `json:"installed"`
	Count       int    `json:"count"`
	Dir         string `json:"dir"`
	Downloading bool   `json:"downloading"`
	Progress    int    `json:"progress"`
	Message     string `json:"message"`
	Error       string `json:"error"`
}

func (s *Session) unityFontMatches() (string, int) {
	dir := filepath.Join(s.runtimeDir, "TMP_Font_AssetBundles")
	matches, _ := filepath.Glob(filepath.Join(dir, "arialuni_sdf*"))
	return dir, len(matches)
}

// UnityFontStatus returns whether TMP_Font_AssetBundles are installed and any active download progress.
func (s *Session) UnityFontStatus() UnityFontStatus {
	dir, count := s.unityFontMatches()
	s.unityFontMu.Lock()
	st := s.unityFontState
	s.unityFontMu.Unlock()

	st.Dir = dir
	st.Count = count
	st.Installed = count > 0
	return st
}

func (s *Session) updateUnityFontState(fn func(*UnityFontStatus)) {
	dir, count := s.unityFontMatches()
	s.unityFontMu.Lock()
	fn(&s.unityFontState)
	st := s.unityFontState
	s.unityFontMu.Unlock()

	st.Dir = dir
	st.Count = count
	st.Installed = count > 0
	s.emit("unityfonts:progress", st)
}

// DownloadUnityFonts downloads TMP_Font_AssetBundles.zip from GitHub Releases and extracts it
// into runtime/TMP_Font_AssetBundles.
func (s *Session) DownloadUnityFonts(ctx context.Context) error {
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

	err := s.runUnityFontDownload(ctx)
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
	return err
}

func (s *Session) runUnityFontDownload(ctx context.Context) error {
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
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmpZip.Write(buf[:n]); werr != nil {
				tmpZip.Close()
				return werr
			}
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

	s.updateUnityFontState(func(st *UnityFontStatus) {
		st.Progress = 92
		st.Message = "Extracting bundles..."
	})

	destDir := filepath.Join(s.runtimeDir, "TMP_Font_AssetBundles")
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

	tmpPath := dstPath + ".tmp"
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dstPath)
}
