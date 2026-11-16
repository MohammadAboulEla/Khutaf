//go:build windows

package hook

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// Inject loads LunaHook{32,64}.dll into pids via LunaSubprocess{32,64}.exe.
// If the normal run fails (typically access denied) it retries elevated (UAC).
func Inject(runtimeDir string, pids []uint32, is64 bool) error {
	if len(pids) == 0 {
		return nil
	}
	arch := "32"
	if is64 {
		arch = "64"
	}
	exe := filepath.Join(runtimeDir, "KhutafSubprocess"+arch+".exe")
	dll := filepath.Join(runtimeDir, "KhutafHook"+arch+".dll")
	for _, p := range []string{exe, dll} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("missing runtime file: %w", err)
		}
	}

	args := []string{"dllinject"}
	for _, pid := range pids {
		args = append(args, strconv.FormatUint(uint64(pid), 10))
	}
	args = append(args, dll)

	cmd := exec.Command(exe, args...)
	cmd.Dir = runtimeDir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	err := cmd.Run()
	// dllinject returns 1 on success, 0 when OpenProcess/VirtualAllocEx failed.
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil
		}
	}
	return injectElevated(exe, args, runtimeDir)
}

func injectElevated(exe string, args []string, dir string) error {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = syscall.EscapeArg(a)
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	wd, _ := windows.UTF16PtrFromString(dir)
	if err := windows.ShellExecute(0, verb, file, params, wd, windows.SW_HIDE); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return errors.New("injection needs administrator rights; run Khutaf as administrator")
		}
		return fmt.Errorf("elevated injection: %w", err)
	}
	return nil
}
