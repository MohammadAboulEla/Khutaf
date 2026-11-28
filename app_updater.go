//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"khutaf/internal/updater"
)

func (a *App) updateLoop(ctx context.Context, stop <-chan struct{}) {
	initial := time.NewTimer(5 * time.Second)
	defer initial.Stop()
	select {
	case <-stop:
		return
	case <-ctx.Done():
		return
	case <-initial.C:
	}

	runSilentCheck := func() {
		if a.updater == nil || a.session == nil {
			return
		}
		if !a.session.Config().Update.AutoCheck {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		_, _ = a.updater.Check(cctx, false)
		cancel()
	}

	runSilentCheck()

	ticker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			runSilentCheck()
		}
	}
}

// GetUpdateStatus returns the current auto-update status snapshot.
func (a *App) GetUpdateStatus() updater.Status {
	if a.updater == nil {
		return updater.Status{State: updater.StateIdle, CurrentVersion: AppVersion, ReleaseURL: updater.DefaultReleasesPageURL}
	}
	return a.updater.Status()
}

// CheckForUpdates checks GitHub Releases for a newer version.
func (a *App) CheckForUpdates(manual bool) (updater.Status, error) {
	if a.updater == nil {
		return a.GetUpdateStatus(), errors.New("updater not initialized")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	return a.updater.Check(ctx, manual)
}

// DownloadUpdate downloads, verifies, and stages the available update release.
func (a *App) DownloadUpdate() error {
	if a.updater == nil {
		return errors.New("updater not initialized")
	}
	return a.updater.Download(a.ctx)
}

// CancelUpdateDownload cancels an in-progress update download.
func (a *App) CancelUpdateDownload() {
	if a.updater != nil {
		a.updater.CancelDownload()
	}
}

// InstallUpdate launches the isolated self-updater and exits Khutaf cleanly to swap binaries.
func (a *App) InstallUpdate(detachIfAttached bool) error {
	if a.updater == nil {
		return errors.New("updater not initialized")
	}
	if a.session != nil && a.session.Status().Attached {
		if !detachIfAttached {
			return errors.New("game_attached")
		}
		a.session.Detach()
	}
	helperExe, args, err := a.updater.PrepareApply()
	if err != nil {
		return err
	}
	if err := updater.LaunchHelper(helperExe, args); err != nil {
		return err
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		a.QuitApp()
	}()
	return nil
}

// SkipUpdateVersion persists a skipped version so silent checks do not re-notify for it.
func (a *App) SkipUpdateVersion(version string) error {
	a.updateMu.Lock()
	cfg := a.session.Config()
	cfg.Update.SkippedVersion = strings.TrimPrefix(strings.TrimSpace(version), "v")
	err := a.session.SaveConfig(cfg)
	a.updateMu.Unlock()
	if err == nil && a.updater != nil {
		a.updater.MarkSkipped(version)
	}
	return err
}

// OpenReleasePage opens the GitHub release page in the user's default browser.
func (a *App) OpenReleasePage(rawURL string) {
	target := updater.DefaultReleasesPageURL
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(trimmed, "https://github.com/MohammadAboulEla/Khutaf/releases") {
		target = trimmed
	}
	if a.ctx != nil {
		runtime.BrowserOpenURL(a.ctx, target)
	}
}
