//go:build windows

package updater

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

const updateErrorFile = ".khutaf-update-error.txt"

// CleanupOldFiles removes leftover .old binaries, staging directories, and temp updater executables.
func CleanupOldFiles(installDir string) {
	if strings.TrimSpace(installDir) != "" {
		_ = os.RemoveAll(filepath.Join(installDir, stageDirName))
		for _, dir := range []string{installDir, filepath.Join(installDir, "runtime")} {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".old") {
					_ = os.Remove(filepath.Join(dir, e.Name()))
				}
			}
		}
	}

	if matches, err := filepath.Glob(filepath.Join(os.TempDir(), "Khutaf-updater-*.exe")); err == nil {
		for _, m := range matches {
			_ = os.Remove(m)
		}
	}
}

// ReadAndClearUpdateError returns and deletes any rollback error left by a failed apply step.
func ReadAndClearUpdateError(installDir string) string {
	if strings.TrimSpace(installDir) == "" {
		return ""
	}
	p := filepath.Join(installDir, updateErrorFile)
	raw, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	_ = os.Remove(p)
	return strings.TrimSpace(string(raw))
}

// CheckApplyFlag executes headless self-update replacement when launched with --apply-update.
func CheckApplyFlag(args []string) (handled bool, exitCode int) {
	isApply := false
	var pid int
	var targetDir, stageDir, fromVersion string

	for _, arg := range args {
		switch {
		case arg == "--apply-update":
			isApply = true
		case strings.HasPrefix(arg, "--pid="):
			pid, _ = strconv.Atoi(strings.TrimPrefix(arg, "--pid="))
		case strings.HasPrefix(arg, "--target="):
			targetDir = strings.TrimPrefix(arg, "--target=")
		case strings.HasPrefix(arg, "--stage="):
			stageDir = strings.TrimPrefix(arg, "--stage=")
		case strings.HasPrefix(arg, "--from="):
			fromVersion = strings.TrimPrefix(arg, "--from=")
		}
	}

	if !isApply {
		return false, 0
	}
	if err := ApplyUpdate(pid, targetDir, stageDir, fromVersion, true); err != nil {
		return true, 1
	}
	return true, 0
}

type swappedBackup struct {
	dst    string
	backup string
	hadOld bool
}

// ApplyUpdate waits for pid to exit, swaps staged files into targetDir with rollback,
// and optionally relaunches the updated Khutaf.exe.
func ApplyUpdate(pid int, targetDir, stageDir, fromVersion string, relaunch bool) error {
	if strings.TrimSpace(targetDir) == "" || strings.TrimSpace(stageDir) == "" {
		return fmt.Errorf("missing target or stage directory")
	}
	if pid > 0 {
		waitForPID(pid, 15*time.Second)
		time.Sleep(250 * time.Millisecond)
	}

	var relFiles []string
	if err := filepath.WalkDir(stageDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(stageDir, path)
		if rerr != nil {
			return rerr
		}
		relFiles = append(relFiles, rel)
		return nil
	}); err != nil {
		writeUpdateError(targetDir, fmt.Sprintf("read staged files: %v", err))
		if relaunch {
			_ = launchDetached(filepath.Join(targetDir, "Khutaf.exe"), nil)
		}
		return err
	}

	var backups []swappedBackup
	rollback := func(cause error) error {
		for i := len(backups) - 1; i >= 0; i-- {
			b := backups[i]
			_ = os.Remove(b.dst)
			if b.hadOld {
				_ = os.Rename(b.backup, b.dst)
			}
		}
		writeUpdateError(targetDir, cause.Error())
		if relaunch {
			_ = launchDetached(filepath.Join(targetDir, "Khutaf.exe"), nil)
		}
		return cause
	}

	for _, rel := range relFiles {
		src := filepath.Join(stageDir, rel)
		dst := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return rollback(fmt.Errorf("create directory for %s: %w", rel, err))
		}

		backup := dst + ".old"
		hadOld := false
		if _, err := os.Stat(dst); err == nil {
			_ = os.Remove(backup)
			if err := renameWithRetry(dst, backup); err != nil {
				return rollback(fmt.Errorf("backup %s: %w", rel, err))
			}
			hadOld = true
		}
		backups = append(backups, swappedBackup{dst: dst, backup: backup, hadOld: hadOld})

		if err := moveFile(src, dst); err != nil {
			return rollback(fmt.Errorf("install %s: %w", rel, err))
		}
	}

	_ = os.RemoveAll(stageDir)
	_ = os.Remove(filepath.Join(targetDir, updateErrorFile))

	if relaunch {
		var launchArgs []string
		if strings.TrimSpace(fromVersion) != "" {
			launchArgs = []string{"--updated-from=" + strings.TrimSpace(fromVersion)}
		}
		if err := launchDetached(filepath.Join(targetDir, "Khutaf.exe"), launchArgs); err != nil {
			writeUpdateError(targetDir, fmt.Sprintf("relaunch updated Khutaf: %v", err))
			return err
		}
	}
	return nil
}

// LaunchHelper starts the isolated updater executable detached and without a console window.
func LaunchHelper(helperExe string, args []string) error {
	return launchDetached(helperExe, args)
}

func launchDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = &windows.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	return cmd.Start()
}

func waitForPID(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
}

func renameWithRetry(src, dst string) error {
	var err error
	for i := 0; i < 6; i++ {
		err = os.Rename(src, dst)
		if err == nil {
			return nil
		}
		time.Sleep(120 * time.Millisecond)
	}
	return err
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	_ = os.Remove(src)
	return nil
}

func writeUpdateError(targetDir, msg string) {
	_ = os.WriteFile(filepath.Join(targetDir, updateErrorFile), []byte(msg), 0o644)
}
