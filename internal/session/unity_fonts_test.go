//go:build windows

package session

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func buildTestUnityFontZip(t *testing.T, name string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestDownloadUnityFontsVerifiesSHA256(t *testing.T) {
	payload := []byte("fake-unity-font-bundle")
	zipBytes, digest := buildTestUnityFontZip(t, "arialuni_sdf_u2022", payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	origURL, origSHA := UnityFontBundleURL, UnityFontBundleSHA256
	UnityFontBundleURL = srv.URL
	UnityFontBundleSHA256 = digest
	defer func() {
		UnityFontBundleURL = origURL
		UnityFontBundleSHA256 = origSHA
	}()

	runtimeDir := t.TempDir()
	sess := &Session{runtimeDir: runtimeDir}
	if err := sess.DownloadUnityFonts(context.Background()); err != nil {
		t.Fatalf("DownloadUnityFonts failed: %v", err)
	}

	extracted := filepath.Join(runtimeDir, "TMP_Font_AssetBundles", "arialuni_sdf_u2022")
	got, err := os.ReadFile(extracted)
	if err != nil {
		t.Fatalf("read extracted bundle: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extracted payload = %q, want %q", got, payload)
	}
}

func TestDownloadUnityFontsRejectsBadSHA256(t *testing.T) {
	zipBytes, _ := buildTestUnityFontZip(t, "arialuni_sdf_u2022", []byte("tampered"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()

	origURL, origSHA := UnityFontBundleURL, UnityFontBundleSHA256
	UnityFontBundleURL = srv.URL
	UnityFontBundleSHA256 = strings.Repeat("0", 64)
	defer func() {
		UnityFontBundleURL = origURL
		UnityFontBundleSHA256 = origSHA
	}()

	runtimeDir := t.TempDir()
	sess := &Session{runtimeDir: runtimeDir}
	err := sess.DownloadUnityFonts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("expected SHA-256 mismatch error, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(runtimeDir, "TMP_Font_AssetBundles", "arialuni_sdf_u2022")); !os.IsNotExist(statErr) {
		t.Fatalf("expected mismatch archive not to extract, statErr=%v", statErr)
	}
}
