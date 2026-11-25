package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	StateIdle        = "idle"
	StateChecking    = "checking"
	StateUpToDate    = "up_to_date"
	StateAvailable   = "available"
	StateDownloading = "downloading"
	StateVerifying   = "verifying"
	StateReady       = "ready"
	StateApplying    = "applying"
	StateError       = "error"
)

var (
	// DefaultReleasesAPIURL points to the GitHub Releases API endpoint for Khutaf.
	DefaultReleasesAPIURL = "https://api.github.com/repos/MohammadAboulEla/Khutaf/releases?per_page=15"
	// DefaultReleasesPageURL is the human-facing releases page fallback.
	DefaultReleasesPageURL = "https://github.com/MohammadAboulEla/Khutaf/releases"

	allowedDownloadHosts = map[string]bool{
		"github.com":                           true,
		"objects.githubusercontent.com":        true,
		"release-assets.githubusercontent.com": true,
	}

	// allowTestURLs is enabled in unit tests to allow httptest localhost URLs.
	allowTestURLs = false
)

const (
	stageDirName       = ".khutaf-update-stage"
	maxArchiveZipBytes = 256 << 20
	maxExtractedBytes  = 256 << 20
	maxChecksumsBytes  = 64 << 10
)

// Status describes the current auto-update state exposed to the UI.
type Status struct {
	State           string `json:"state"`
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	TagName         string `json:"tagName"`
	ReleaseName     string `json:"releaseName"`
	ReleaseNotes    string `json:"releaseNotes"`
	ReleaseURL      string `json:"releaseUrl"`
	PublishedAt     string `json:"publishedAt"`
	AssetName       string `json:"assetName"`
	AssetURL        string `json:"assetUrl"`
	AssetSize       int64  `json:"assetSize"`
	AssetSHA256     string `json:"assetSha256"`
	Progress        int    `json:"progress"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	LastCheckedAt   string `json:"lastCheckedAt"`
	Writable        bool   `json:"writable"`
	Prerelease      bool   `json:"prerelease"`
	Error           string `json:"error"`
	UpdatedFrom     string `json:"updatedFrom"`

	ChecksumsURL string `json:"-"`
	StagedDir    string `json:"-"`
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt string    `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Manager coordinates checking, downloading, verifying, and staging app updates.
type Manager struct {
	mu             sync.Mutex
	currentVersion string
	installDir     string
	emit           func(string, any)
	getConfig      func() config.UpdateSettings
	saveMeta       func(lastCheckedAt, etag string)
	httpClient     *http.Client
	downloadCancel context.CancelFunc
	status         Status
}

// New creates an update Manager for the running Khutaf installation.
func New(
	currentVersion string,
	installDir string,
	emit func(string, any),
	getConfig func() config.UpdateSettings,
	saveMeta func(lastCheckedAt, etag string),
) *Manager {
	var lastChecked string
	if getConfig != nil {
		lastChecked = getConfig().LastCheckedAt
	}
	return &Manager{
		currentVersion: strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
		installDir:     installDir,
		emit:           emit,
		getConfig:      getConfig,
		saveMeta:       saveMeta,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		status: Status{
			State:          StateIdle,
			CurrentVersion: strings.TrimPrefix(strings.TrimSpace(currentVersion), "v"),
			ReleaseURL:     DefaultReleasesPageURL,
			LastCheckedAt:  lastChecked,
			Writable:       isDirWritable(installDir),
		},
	}
}

// SetUpdatedFrom records the previous version when launched immediately after an update.
func (m *Manager) SetUpdatedFrom(prev string) {
	m.mu.Lock()
	m.status.UpdatedFrom = strings.TrimPrefix(strings.TrimSpace(prev), "v")
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)
}

// SetStartupError surfaces a post-rollback update failure message.
func (m *Manager) SetStartupError(errMsg string) {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return
	}
	m.mu.Lock()
	m.status.State = StateError
	m.status.Error = errMsg
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)
}

// Status returns a snapshot of the current update status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) emitStatus(st Status) {
	if m.emit != nil {
		m.emit("updater:status", st)
	}
}

func (m *Manager) currentUpdateSettings() config.UpdateSettings {
	if m.getConfig != nil {
		return m.getConfig()
	}
	return config.UpdateSettings{AutoCheck: true, IncludePrerelease: true}
}

// Check queries GitHub Releases for a newer Khutaf release.
func (m *Manager) Check(ctx context.Context, manual bool) (Status, error) {
	m.mu.Lock()
	if m.status.State == StateDownloading || m.status.State == StateVerifying || m.status.State == StateApplying {
		st := m.status
		m.mu.Unlock()
		return st, nil
	}
	prevStatus := m.status
	m.status.State = StateChecking
	m.status.Error = ""
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)

	cfg := m.currentUpdateSettings()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DefaultReleasesAPIURL, nil)
	if err != nil {
		return m.failCheck(err, manual, prevStatus)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Khutaf/"+m.currentVersion)
	if !manual && strings.TrimSpace(cfg.ETag) != "" {
		req.Header.Set("If-None-Match", strings.TrimSpace(cfg.ETag))
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return m.failCheck(err, manual, prevStatus)
	}
	defer resp.Body.Close()

	nowStr := time.Now().UTC().Format(time.RFC3339)

	if resp.StatusCode == http.StatusNotModified {
		if m.saveMeta != nil {
			m.saveMeta(nowStr, cfg.ETag)
		}
		m.mu.Lock()
		m.status.LastCheckedAt = nowStr
		if prevStatus.LatestVersion != "" && prevStatus.State == StateAvailable {
			m.status.State = StateAvailable
		} else if prevStatus.State == StateReady {
			m.status.State = StateReady
		} else {
			m.status.State = StateUpToDate
		}
		st := m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return st, nil
	}

	if resp.StatusCode != http.StatusOK {
		return m.failCheck(fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode), manual, prevStatus)
	}

	var releases []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&releases); err != nil {
		return m.failCheck(fmt.Errorf("decode releases response: %w", err), manual, prevStatus)
	}

	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if m.saveMeta != nil {
		m.saveMeta(nowStr, etag)
	}

	currentVer, err := ParseVersion(m.currentVersion)
	if err != nil {
		return m.failCheck(fmt.Errorf("parse current version %q: %w", m.currentVersion, err), manual, prevStatus)
	}

	best, bestVer, found := selectLatestRelease(releases, cfg.IncludePrerelease)
	if !found {
		m.mu.Lock()
		m.status.State = StateUpToDate
		m.status.LastCheckedAt = nowStr
		m.status.Error = ""
		st := m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return st, nil
	}

	if bestVer.Compare(currentVer) <= 0 {
		m.mu.Lock()
		m.status.State = StateUpToDate
		m.status.LatestVersion = bestVer.Raw
		m.status.TagName = best.release.TagName
		m.status.ReleaseURL = fallbackURL(best.release.HTMLURL)
		m.status.LastCheckedAt = nowStr
		m.status.Error = ""
		st := m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return st, nil
	}

	// Respect skipped version on silent background checks.
	if !manual && strings.TrimSpace(cfg.SkippedVersion) != "" {
		if skippedVer, serr := ParseVersion(cfg.SkippedVersion); serr == nil && bestVer.Compare(skippedVer) == 0 {
			m.mu.Lock()
			m.status.State = StateUpToDate
			m.status.LatestVersion = bestVer.Raw
			m.status.TagName = best.release.TagName
			m.status.ReleaseURL = fallbackURL(best.release.HTMLURL)
			m.status.LastCheckedAt = nowStr
			m.status.Error = ""
			st := m.status
			m.mu.Unlock()
			m.emitStatus(st)
			return st, nil
		}
	}

	m.mu.Lock()
	alreadyReady := prevStatus.State == StateReady && prevStatus.LatestVersion == bestVer.Raw && prevStatus.StagedDir != ""
	nextState := StateAvailable
	if alreadyReady {
		nextState = StateReady
	}
	m.status = Status{
		State:          nextState,
		CurrentVersion: m.currentVersion,
		LatestVersion:  bestVer.Raw,
		TagName:        best.release.TagName,
		ReleaseName:    strings.TrimSpace(best.release.Name),
		ReleaseNotes:   strings.TrimSpace(best.release.Body),
		ReleaseURL:     fallbackURL(best.release.HTMLURL),
		PublishedAt:    best.release.PublishedAt,
		AssetName:      best.zipAsset.Name,
		AssetURL:       best.zipAsset.BrowserDownloadURL,
		AssetSize:      best.zipAsset.Size,
		AssetSHA256:    parseSHA256Digest(best.zipAsset.Digest),
		TotalBytes:     best.zipAsset.Size,
		LastCheckedAt:  nowStr,
		Writable:       isDirWritable(m.installDir),
		Prerelease:     best.release.Prerelease || bestVer.Prerelease != "",
		UpdatedFrom:    prevStatus.UpdatedFrom,
		ChecksumsURL:   best.checksumsURL,
		StagedDir:      prevStatus.StagedDir,
	}
	if alreadyReady {
		m.status.Progress = 100
		m.status.DownloadedBytes = best.zipAsset.Size
	}
	st = m.status
	m.mu.Unlock()
	m.emitStatus(st)
	return st, nil
}

func (m *Manager) failCheck(err error, manual bool, prev Status) (Status, error) {
	m.mu.Lock()
	if manual {
		m.status.State = StateError
		m.status.Error = err.Error()
	} else {
		// Keep silent background checks non-disruptive if offline or rate-limited.
		if prev.State == StateAvailable || prev.State == StateReady {
			m.status.State = prev.State
		} else {
			m.status.State = StateIdle
		}
		m.status.Error = ""
	}
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)
	return st, err
}

type releaseCandidate struct {
	release      ghRelease
	zipAsset     ghAsset
	checksumsURL string
}

func selectLatestRelease(releases []ghRelease, includePrerelease bool) (releaseCandidate, Version, bool) {
	var best releaseCandidate
	var bestVer Version
	found := false

	for _, rel := range releases {
		if rel.Draft {
			continue
		}
		zipAsset, checksumsURL, ok := findReleaseAssets(rel.Assets)
		if !ok {
			continue
		}
		verStr := strings.TrimSpace(rel.TagName)
		if assetVer := ExtractAssetVersion(zipAsset.Name); assetVer != "" {
			// Prefer asset version when tag omits prerelease suffix (e.g. tag v0.1.5 with Khutaf-v0.1.5-beta-win64.zip).
			tagV, tagErr := ParseVersion(verStr)
			assetV, assetErr := ParseVersion(assetVer)
			if assetErr == nil && (tagErr != nil || (tagV.Major == assetV.Major && tagV.Minor == assetV.Minor && tagV.Patch == assetV.Patch && tagV.Prerelease == "" && assetV.Prerelease != "")) {
				verStr = assetVer
			}
		}
		ver, err := ParseVersion(verStr)
		if err != nil {
			continue
		}
		isPre := rel.Prerelease || ver.Prerelease != ""
		if isPre && !includePrerelease {
			continue
		}
		if !found || ver.Compare(bestVer) > 0 {
			best = releaseCandidate{
				release:      rel,
				zipAsset:     zipAsset,
				checksumsURL: checksumsURL,
			}
			bestVer = ver
			found = true
		}
	}
	return best, bestVer, found
}

func findReleaseAssets(assets []ghAsset) (ghAsset, string, bool) {
	var zipAsset ghAsset
	var checksumsURL string
	foundZip := false

	for _, a := range assets {
		nameLower := strings.ToLower(strings.TrimSpace(a.Name))
		if nameLower == "sha256sums.txt" || nameLower == "sha256sums" {
			checksumsURL = a.BrowserDownloadURL
			continue
		}
		if !foundZip && strings.HasPrefix(nameLower, "khutaf-") && strings.HasSuffix(nameLower, "-win64.zip") {
			zipAsset = a
			foundZip = true
		}
	}
	return zipAsset, checksumsURL, foundZip
}

func parseSHA256Digest(digest string) string {
	digest = strings.TrimSpace(digest)
	if strings.HasPrefix(strings.ToLower(digest), "sha256:") {
		hexPart := strings.TrimSpace(digest[len("sha256:"):])
		if len(hexPart) == 64 {
			return strings.ToLower(hexPart)
		}
	}
	return ""
}

func fallbackURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return DefaultReleasesPageURL
	}
	return u
}

func isDirWritable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	f, err := os.CreateTemp(dir, ".khutaf-upd-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if allowTestURLs && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		return nil
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("refusing non-HTTPS update URL %q", rawURL)
	}
	host := strings.ToLower(parsed.Hostname())
	if !allowedDownloadHosts[host] {
		return fmt.Errorf("refusing untrusted update host %q", host)
	}
	return nil
}

// Download downloads, verifies, and stages the currently available update archive.
func (m *Manager) Download(ctx context.Context) error {
	m.mu.Lock()
	if m.status.State == StateDownloading || m.status.State == StateVerifying || m.status.State == StateApplying {
		m.mu.Unlock()
		return errors.New("update download already in progress")
	}
	if m.status.AssetURL == "" || m.status.LatestVersion == "" {
		m.mu.Unlock()
		return errors.New("no update available to download")
	}
	if !isDirWritable(m.installDir) {
		m.status.Writable = false
		m.status.State = StateError
		m.status.Error = fmt.Sprintf("install folder %s is not writable", m.installDir)
		st := m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return errors.New(st.Error)
	}

	dlCtx, cancel := context.WithCancel(ctx)
	m.downloadCancel = cancel
	assetURL := m.status.AssetURL
	assetName := m.status.AssetName
	expectedSHA := m.status.AssetSHA256
	checksumsURL := m.status.ChecksumsURL
	totalBytes := m.status.AssetSize

	m.status.State = StateDownloading
	m.status.Writable = true
	m.status.Progress = 0
	m.status.DownloadedBytes = 0
	m.status.TotalBytes = totalBytes
	m.status.Error = ""
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)

	defer func() {
		cancel()
		m.mu.Lock()
		m.downloadCancel = nil
		m.mu.Unlock()
	}()

	err := m.runDownload(dlCtx, assetURL, assetName, expectedSHA, checksumsURL)
	if err != nil {
		m.mu.Lock()
		if errors.Is(err, context.Canceled) {
			m.status.State = StateAvailable
			m.status.Progress = 0
			m.status.DownloadedBytes = 0
			m.status.Error = ""
		} else {
			m.status.State = StateError
			m.status.Error = err.Error()
		}
		st = m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return err
	}
	return nil
}

// CancelDownload aborts an in-flight update download.
func (m *Manager) CancelDownload() {
	m.mu.Lock()
	cancel := m.downloadCancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// MarkSkipped suppresses notifications for the specified version without a network check.
func (m *Manager) MarkSkipped(version string) {
	m.mu.Lock()
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v != "" && strings.EqualFold(strings.TrimPrefix(m.status.LatestVersion, "v"), v) {
		if m.status.State == StateAvailable || m.status.State == StateError {
			m.status.State = StateUpToDate
			m.status.Error = ""
		}
	}
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)
}

func (m *Manager) runDownload(ctx context.Context, assetURL, assetName, expectedSHA, checksumsURL string) error {
	if err := validateDownloadURL(assetURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Khutaf/"+m.currentVersion)

	client := &http.Client{
		Timeout:   15 * time.Minute,
		Transport: m.httpClient.Transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return errors.New("too many redirects")
			}
			return validateDownloadURL(req.URL.String())
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "khutaf-update-*.zip")
	if err != nil {
		return err
	}
	tmpZipPath := tmpZip.Name()
	defer os.Remove(tmpZipPath)

	total := resp.ContentLength
	if total <= 0 {
		total = m.Status().AssetSize
	}

	var downloaded int64
	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	lastEmit := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if downloaded+int64(n) > maxArchiveZipBytes {
				tmpZip.Close()
				return fmt.Errorf("update archive exceeds %d MB limit", maxArchiveZipBytes>>20)
			}
			if _, werr := tmpZip.Write(buf[:n]); werr != nil {
				tmpZip.Close()
				return werr
			}
			_, _ = hasher.Write(buf[:n])
			downloaded += int64(n)
			if time.Since(lastEmit) >= 120*time.Millisecond {
				lastEmit = time.Now()
				pct := 0
				if total > 0 {
					pct = int((downloaded * 90) / total)
					if pct > 90 {
						pct = 90
					}
				}
				m.mu.Lock()
				m.status.Progress = pct
				m.status.DownloadedBytes = downloaded
				if total > 0 {
					m.status.TotalBytes = total
				}
				st := m.status
				m.mu.Unlock()
				m.emitStatus(st)
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

	m.mu.Lock()
	m.status.State = StateVerifying
	m.status.Progress = 92
	m.status.DownloadedBytes = downloaded
	if total > 0 {
		m.status.TotalBytes = total
	}
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)

	if expectedSHA == "" && checksumsURL != "" {
		if fetched, ferr := fetchChecksumForAsset(ctx, client, checksumsURL, assetName, m.currentVersion); ferr == nil && fetched != "" {
			expectedSHA = fetched
		}
	}

	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actualSHA, expectedSHA) {
		return fmt.Errorf("update SHA-256 mismatch: got %s, expected %s", actualSHA, expectedSHA)
	}

	stageDir := filepath.Join(m.installDir, stageDirName)
	_ = os.RemoveAll(stageDir)
	if err := extractUpdateZip(tmpZipPath, stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return err
	}

	if err := validateStagedPayload(stageDir); err != nil {
		_ = os.RemoveAll(stageDir)
		return err
	}

	m.mu.Lock()
	m.status.State = StateReady
	m.status.Progress = 100
	m.status.AssetSHA256 = actualSHA
	m.status.StagedDir = stageDir
	m.status.Error = ""
	st = m.status
	m.mu.Unlock()
	m.emitStatus(st)
	return nil
}

func fetchChecksumForAsset(ctx context.Context, client *http.Client, checksumsURL, assetName, userAgentVer string) (string, error) {
	if err := validateDownloadURL(checksumsURL); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Khutaf/"+userAgentVer)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksumsBytes))
	if err != nil {
		return "", err
	}
	return parseChecksumsText(string(raw), assetName), nil
}

func parseChecksumsText(content, assetName string) string {
	target := strings.ToLower(strings.TrimSpace(assetName))
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		hash := strings.TrimSpace(fields[0])
		file := strings.TrimPrefix(strings.TrimSpace(fields[len(fields)-1]), "*")
		if len(hash) == 64 && strings.EqualFold(filepath.Base(file), target) {
			return strings.ToLower(hash)
		}
	}
	return ""
}

func extractUpdateZip(zipPath, stageDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open update archive: %w", err)
	}
	defer zr.Close()

	absStage, err := filepath.Abs(stageDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absStage, 0o755); err != nil {
		return err
	}

	var totalWritten int64
	for _, f := range zr.File {
		rel, ok := safeArchiveRelPath(f.Name)
		if !ok {
			return fmt.Errorf("unsafe path in update archive: %q", f.Name)
		}
		if isProtectedUserPath(rel) {
			continue
		}
		dstPath := filepath.Join(absStage, rel)
		if !isPathInsideDir(dstPath, absStage) {
			return fmt.Errorf("archive entry escapes staging directory: %q", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			return err
		}

		remaining := maxExtractedBytes - totalWritten
		n, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
		closeOutErr := out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
		totalWritten += n
		if totalWritten > maxExtractedBytes {
			return fmt.Errorf("extracted archive exceeds %d MB limit", maxExtractedBytes>>20)
		}
	}
	return nil
}

func safeArchiveRelPath(entry string) (string, bool) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(entry), "\\", "/")
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "" || strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	// Reject Windows drive-letter paths such as C:/foo.
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		return "", false
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", false
		}
	}
	rel := filepath.Clean(filepath.FromSlash(cleaned))
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func isProtectedUserPath(rel string) bool {
	norm := strings.ToLower(filepath.ToSlash(rel))
	return norm == "userconfig" ||
		strings.HasPrefix(norm, "userconfig/") ||
		norm == "runtime/tmp_font_assetbundles" ||
		strings.HasPrefix(norm, "runtime/tmp_font_assetbundles/")
}

func isPathInsideDir(target, dir string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func validateStagedPayload(stageDir string) error {
	exePath := filepath.Join(stageDir, "Khutaf.exe")
	if info, err := os.Stat(exePath); err != nil || info.IsDir() || info.Size() == 0 {
		return errors.New("update archive is missing Khutaf.exe")
	}
	dllPath := filepath.Join(stageDir, "runtime", "khutaf-hook64.dll")
	if info, err := os.Stat(dllPath); err != nil || info.IsDir() || info.Size() == 0 {
		return errors.New("update archive is missing runtime/khutaf-hook64.dll")
	}
	return nil
}

// PrepareApply copies the staged executable to %TEMP% as an isolated helper process
// and returns the executable path and CLI arguments to complete the swap after exit.
func (m *Manager) PrepareApply() (string, []string, error) {
	m.mu.Lock()
	if m.status.State != StateReady || m.status.StagedDir == "" {
		m.mu.Unlock()
		return "", nil, errors.New("no staged update is ready to install")
	}
	stageDir := m.status.StagedDir
	m.status.State = StateApplying
	m.status.Error = ""
	st := m.status
	m.mu.Unlock()
	m.emitStatus(st)

	stagedExe := filepath.Join(stageDir, "Khutaf.exe")
	helperExe := filepath.Join(os.TempDir(), fmt.Sprintf("Khutaf-updater-%d.exe", os.Getpid()))
	if err := copyFile(stagedExe, helperExe); err != nil {
		m.mu.Lock()
		m.status.State = StateReady
		m.status.Error = err.Error()
		st = m.status
		m.mu.Unlock()
		m.emitStatus(st)
		return "", nil, fmt.Errorf("prepare updater helper: %w", err)
	}

	args := []string{
		"--apply-update",
		fmt.Sprintf("--pid=%d", os.Getpid()),
		"--target=" + m.installDir,
		"--stage=" + stageDir,
		"--from=" + m.currentVersion,
	}
	return helperExe, args, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
