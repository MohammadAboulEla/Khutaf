//go:build windows

package hook

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const nativeHookDLL = "khutaf-hook64.dll"

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procLoadLibraryW       = kernel32.NewProc("LoadLibraryW")
	procVirtualAllocEx     = kernel32.NewProc("VirtualAllocEx")
	procVirtualFreeEx      = kernel32.NewProc("VirtualFreeEx")
	procCreateRemoteThread = kernel32.NewProc("CreateRemoteThread")
)

// Inject loads khutaf-hook64.dll into every target pid via CreateRemoteThread(LoadLibraryW).
func Inject(runtimeDir string, pids []uint32, is64 bool) error {
	if len(pids) == 0 {
		return nil
	}
	if !is64 {
		return errors.New("32-bit games are not supported yet (x64 Unity only)")
	}
	dllPath := filepath.Join(runtimeDir, nativeHookDLL)
	if _, err := os.Stat(dllPath); err != nil {
		return fmt.Errorf("missing runtime file: %w", err)
	}
	absDLL, err := filepath.Abs(dllPath)
	if err != nil {
		return fmt.Errorf("resolve runtime path: %w", err)
	}
	if err := procLoadLibraryW.Find(); err != nil {
		return fmt.Errorf("resolve LoadLibraryW: %w", err)
	}
	loadLibAddr := procLoadLibraryW.Addr()

	var errs []error
	for _, pid := range pids {
		if isModuleLoaded(pid, nativeHookDLL) {
			continue
		}
		if err := injectPID(pid, absDLL, loadLibAddr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func injectPID(pid uint32, dllPath string, loadLibAddr uintptr) error {
	const access = windows.PROCESS_CREATE_THREAD |
		windows.PROCESS_QUERY_INFORMATION |
		windows.PROCESS_VM_OPERATION |
		windows.PROCESS_VM_WRITE |
		windows.PROCESS_VM_READ

	proc, err := windows.OpenProcess(access, false, pid)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return errors.New("access denied opening game process; please run Khutaf as administrator")
		}
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(proc)

	utf16, err := windows.UTF16FromString(dllPath)
	if err != nil {
		return fmt.Errorf("encode dll path: %w", err)
	}
	byteLen := uintptr(len(utf16) * 2)

	remoteBuf, _, allocErr := procVirtualAllocEx.Call(
		uintptr(proc),
		0,
		byteLen,
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_READWRITE,
	)
	if remoteBuf == 0 {
		return fmt.Errorf("VirtualAllocEx(%d): %w", pid, allocErr)
	}
	freeRemote := true
	defer func() {
		if freeRemote {
			_, _, _ = procVirtualFreeEx.Call(uintptr(proc), remoteBuf, 0, windows.MEM_RELEASE)
		}
	}()

	var written uintptr
	err = windows.WriteProcessMemory(proc, remoteBuf, (*byte)(unsafe.Pointer(&utf16[0])), byteLen, &written)
	runtime.KeepAlive(utf16)
	if err != nil {
		return fmt.Errorf("WriteProcessMemory(%d): %w", pid, err)
	}

	thread, _, threadErr := procCreateRemoteThread.Call(
		uintptr(proc),
		0,
		0,
		loadLibAddr,
		remoteBuf,
		0,
		0,
	)
	if thread == 0 {
		return fmt.Errorf("CreateRemoteThread(%d): %w", pid, threadErr)
	}
	defer windows.CloseHandle(windows.Handle(thread))

	event, err := windows.WaitForSingleObject(windows.Handle(thread), 15000)
	if err != nil {
		freeRemote = false
		return fmt.Errorf("wait LoadLibraryW(%d): %w", pid, err)
	}
	if event != windows.WAIT_OBJECT_0 {
		freeRemote = false
		if isModuleLoaded(pid, nativeHookDLL) {
			return nil
		}
		return fmt.Errorf("LoadLibraryW(%d) timed out (wait status %d)", pid, event)
	}
	if !isModuleLoaded(pid, nativeHookDLL) {
		return fmt.Errorf("LoadLibraryW(%d) failed to load %s", pid, nativeHookDLL)
	}
	return nil
}

func isModuleLoaded(pid uint32, moduleName string) bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var me windows.ModuleEntry32
	me.Size = uint32(unsafe.Sizeof(me))
	for err = windows.Module32First(snap, &me); err == nil; err = windows.Module32Next(snap, &me) {
		if strings.EqualFold(windows.UTF16ToString(me.Module[:]), moduleName) {
			return true
		}
	}
	return false
}

// DetectAntiCheat checks the game directory and loaded modules of pids for common
// anti-cheat drivers or runtime DLLs and returns the product name, or "" if none match.
func DetectAntiCheat(pids []uint32, exePath string) string {
	dirMarkers := []struct {
		rel  string
		name string
	}{
		{"EasyAntiCheat", "EasyAntiCheat"},
		{"EasyAntiCheat_EOS", "EasyAntiCheat"},
		{"BattlEye", "BattlEye"},
	}
	if exePath != "" {
		gameDir := filepath.Dir(exePath)
		for _, m := range dirMarkers {
			if _, err := os.Stat(filepath.Join(gameDir, m.rel)); err == nil {
				return m.name
			}
		}
	}

	modPatterns := []struct {
		substr string
		name   string
	}{
		{"easyanticheat", "EasyAntiCheat"},
		{"beclient", "BattlEye"},
		{"beservice", "BattlEye"},
		{"vgk", "Riot Vanguard"},
		{"vgc", "Riot Vanguard"},
		{"mhyprot", "miHoYo Anti-Cheat"},
		{"ace-base", "ACE Anti-Cheat"},
		{"gameguard", "nProtect GameGuard"},
		{"nprotect", "nProtect GameGuard"},
		{"xigncode", "XIGNCODE3"},
	}
	for _, pid := range pids {
		snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
		if err != nil {
			continue
		}
		var me windows.ModuleEntry32
		me.Size = uint32(unsafe.Sizeof(me))
		for err = windows.Module32First(snap, &me); err == nil; err = windows.Module32Next(snap, &me) {
			mod := strings.ToLower(windows.UTF16ToString(me.Module[:]))
			for _, p := range modPatterns {
				if strings.Contains(mod, p.substr) {
					_ = windows.CloseHandle(snap)
					return p.name
				}
			}
		}
		_ = windows.CloseHandle(snap)
	}
	return ""
}
