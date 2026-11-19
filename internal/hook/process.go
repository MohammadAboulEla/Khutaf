//go:build windows

package hook

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessInfo describes a running process with a visible top-level window.
type ProcessInfo struct {
	PID       uint32 `json:"pid"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Is64      bool   `json:"is64"`
	Engine    string `json:"engine"` // "unity-il2cpp", "unity-mono", "unity", or ""
	Supported bool   `json:"supported"`
}

var (
	user32                   = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procIsWindowVisible      = user32.NewProc("IsWindowVisible")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindow            = user32.NewProc("GetWindow")

	enumMu      sync.Mutex
	enumResults map[uint32]string // pid -> first window title
	cbEnumWnd   = syscall.NewCallback(enumWindowProc)
)

const gwOwner = 4

func enumWindowProc(hwnd, _ uintptr) uintptr {
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		return 1
	}
	if owner, _, _ := procGetWindow.Call(hwnd, gwOwner); owner != 0 {
		return 1
	}
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return 1
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	if _, exists := enumResults[pid]; !exists {
		enumResults[pid] = windows.UTF16ToString(buf)
	}
	return 1
}

// ListProcesses returns user processes that own a visible titled window,
// excluding system processes and this process.
func ListProcesses() ([]ProcessInfo, error) {
	enumMu.Lock()
	enumResults = map[uint32]string{}
	procEnumWindows.Call(cbEnumWnd, 0)
	titles := enumResults
	enumResults = nil
	enumMu.Unlock()

	self := uint32(os.Getpid())
	winDir := strings.ToLower(os.Getenv("SystemRoot"))
	var list []ProcessInfo
	for pid, title := range titles {
		if pid == self || pid == 0 {
			continue
		}
		path, err := ProcessPath(pid)
		if err != nil || path == "" {
			continue
		}
		lower := strings.ToLower(path)
		if (winDir != "" && strings.HasPrefix(lower, winDir+`\`)) || strings.Contains(lower, `\windowsapps\`) {
			continue
		}
		is64, _ := Is64Bit(pid)
		engine := DetectEngine(path)
		list = append(list, ProcessInfo{
			PID:       pid,
			Name:      filepath.Base(path),
			Path:      path,
			Title:     title,
			Is64:      is64,
			Engine:    engine,
			Supported: is64 && strings.HasPrefix(engine, "unity"),
		})
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
	return list, nil
}

// ProcessPath returns the full executable path of pid.
func ProcessPath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// Is64Bit reports whether pid is a native 64-bit process.
func Is64Bit(pid uint32) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	var wow64 bool
	if err := windows.IsWow64Process(h, &wow64); err != nil {
		return false, err
	}
	return !wow64, nil
}

// PIDsByPath returns all running PIDs whose executable matches path.
func PIDsByPath(path string) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	target := filepath.Base(path)
	var pids []uint32
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), target) {
			continue
		}
		if p, perr := ProcessPath(entry.ProcessID); perr == nil && strings.EqualFold(p, path) {
			pids = append(pids, entry.ProcessID)
		}
	}
	return pids
}

// RunningPathsMap takes a single process snapshot and maps normalized lowercase
// executable paths to their first running PID, without requiring a visible window.
func RunningPathsMap() map[string]uint32 {
	return RunningPathsFor(nil)
}

// RunningPathsFor behaves like RunningPathsMap, but when basenames is non-nil it
// only resolves full executable paths for processes whose lowercase exe filename
// is present in basenames.
func RunningPathsFor(basenames map[string]bool) map[string]uint32 {
	out := make(map[string]uint32)
	if basenames != nil && len(basenames) == 0 {
		return out
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if basenames != nil {
			exeName := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
			if !basenames[exeName] {
				continue
			}
		}
		p, perr := ProcessPath(entry.ProcessID)
		if perr != nil || p == "" {
			continue
		}
		norm := strings.ToLower(filepath.Clean(p))
		if _, exists := out[norm]; !exists {
			out[norm] = entry.ProcessID
		}
	}
	return out
}

// DetectEngine inspects the game folder for Unity markers.
func DetectEngine(exePath string) string {
	dir := filepath.Dir(exePath)
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if !exists(filepath.Join(dir, "UnityPlayer.dll")) {
		dataDir := strings.TrimSuffix(exePath, filepath.Ext(exePath)) + "_Data"
		if !exists(dataDir) {
			return ""
		}
	}
	if exists(filepath.Join(dir, "GameAssembly.dll")) {
		return "unity-il2cpp"
	}
	if exists(filepath.Join(dir, "MonoBleedingEdge")) || exists(filepath.Join(dir, "Mono")) {
		return "unity-mono"
	}
	return "unity"
}

// IsAdmin reports whether the current process is running with elevated privileges.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RestartAsAdmin launches a new elevated instance of the current executable.
func RestartAsAdmin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	wd, _ := os.Getwd()
	quoted := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		quoted = append(quoted, syscall.EscapeArg(a))
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	params, _ := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	workDir, _ := windows.UTF16PtrFromString(wd)
	return windows.ShellExecute(0, verb, file, params, workDir, windows.SW_SHOWNORMAL)
}
