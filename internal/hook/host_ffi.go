//go:build windows

package hook

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Host wraps LunaHost64.dll. Only one Host may be started per process because
// the native callbacks are global.
type Host struct {
	runtimeDir string
	dll        *syscall.LazyDLL

	procStart            *syscall.LazyProc
	procSettings         *syscall.LazyProc
	procSettingsEx       *syscall.LazyProc
	procConnectProcess   *syscall.LazyProc
	procCheckIfNeedInj   *syscall.LazyProc
	procDetachProcess    *syscall.LazyProc
	procUseEmbed         *syscall.LazyProc
	procCheckUsingEmbed  *syscall.LazyProc
	procEmbedCallback    *syscall.LazyProc
	procInsertHookCode   *syscall.LazyProc
	procRemoveHook       *syscall.LazyProc
	procInsertPCHooks    *syscall.LazyProc
	procResetLang        *syscall.LazyProc
}

var (
	startOnce sync.Once
	handlerMu sync.RWMutex
	handler   Handler
)

// NewHost loads KhutafHost64.dll from runtimeDir and resolves all exports.
func NewHost(runtimeDir string) (*Host, error) {
	dllPath := filepath.Join(runtimeDir, "KhutafHost64.dll")
	if _, err := os.Stat(dllPath); err != nil {
		return nil, fmt.Errorf("KhutafHost64.dll not found: %w", err)
	}
	dll := syscall.NewLazyDLL(dllPath)
	if err := dll.Load(); err != nil {
		return nil, fmt.Errorf("load %s: %w", dllPath, err)
	}
	h := &Host{runtimeDir: runtimeDir, dll: dll}
	exports := map[string]**syscall.LazyProc{
		"Luna_Start":             &h.procStart,
		"Luna_Settings":          &h.procSettings,
		"Luna_SettingsEx":        &h.procSettingsEx,
		"Luna_ConnectProcess":    &h.procConnectProcess,
		"Luna_CheckIfNeedInject": &h.procCheckIfNeedInj,
		"Luna_DetachProcess":     &h.procDetachProcess,
		"Luna_UseEmbed":          &h.procUseEmbed,
		"Luna_CheckIsUsingEmbed": &h.procCheckUsingEmbed,
		"Luna_EmbedCallback":     &h.procEmbedCallback,
		"Luna_InsertHookCode":    &h.procInsertHookCode,
		"Luna_RemoveHook":        &h.procRemoveHook,
		"Luna_InsertPCHooks":     &h.procInsertPCHooks,
		"Luna_ResetLang":         &h.procResetLang,
	}
	for name, dst := range exports {
		p := dll.NewProc(name)
		if err := p.Find(); err != nil {
			return nil, fmt.Errorf("missing export %s: %w", name, err)
		}
		*dst = p
	}
	return h, nil
}

// RuntimeDir returns the folder holding the native hook binaries.
func (h *Host) RuntimeDir() string { return h.runtimeDir }

// Start registers the native callbacks. Must be called exactly once.
func (h *Host) Start(hd Handler) error {
	if hd == nil {
		return errors.New("nil handler")
	}
	handlerMu.Lock()
	handler = hd
	handlerMu.Unlock()

	started := false
	startOnce.Do(func() {
		started = true
		h.procStart.Call(
			cbProcessConnect,
			cbProcessRemove,
			cbNewHook,
			cbRemoveHook,
			cbOutput,
			cbHostInfo,
			cbHookInsert,
			cbEmbed,
			cbI18NQuery,
			cbEmuGameInfo,
		)
		h.procResetLang.Call()
	})
	if !started {
		return errors.New("host already started")
	}
	return nil
}

// Settings calls Luna_Settings.
func (h *Host) Settings(s HostSettings) {
	h.procSettings.Call(
		uintptr(s.FlushDelayMs),
		uintptr(s.Codepage),
		uintptr(s.MaxBufferSize),
		uintptr(s.MaxHistorySize),
		boolArg(s.EnablePCHooks),
	)
}

// SettingsEx writes the embed configuration into the game's shared memory.
func (h *Host) SettingsEx(pid uint32, s EmbedSettings) {
	font := utf16Ptr(s.FontFamily)
	fontDir := utf16Ptr(s.UnityFontDir)
	relative := s.FontSizeRelative
	if relative <= 0 {
		relative = 1
	}
	h.procSettingsEx.Call(
		uintptr(pid),
		uintptr(s.WaitTimeMs),
		2,          // fontCharSet (unused while fontCharSetEnabled is false)
		boolArg(false), // fontCharSetEnabled
		uintptr(unsafe.Pointer(font)),
		uintptr(s.DisplayMode),
		boolArg(true), // fastskipignore
		boolArg(s.ClearText),
		boolArg(s.ChangeFontSize),
		// 10th argument lives on the stack; callee reads the low 32 bits as float.
		uintptr(math.Float32bits(relative)),
		boolArg(true), // tryvehhook
		uintptr(unsafe.Pointer(fontDir)),
	)
}

// ConnectProcess opens the host-side pipe for pid.
func (h *Host) ConnectProcess(pid uint32) { h.procConnectProcess.Call(uintptr(pid)) }

// CheckIfNeedInject reports whether LunaHook is not yet loaded into pid.
func (h *Host) CheckIfNeedInject(pid uint32) bool {
	r, _, _ := h.procCheckIfNeedInj.Call(uintptr(pid))
	return byte(r) != 0
}

// DetachProcess unhooks pid.
func (h *Host) DetachProcess(pid uint32) { h.procDetachProcess.Call(uintptr(pid)) }

// UseEmbed enables or disables embedded translation for a text thread.
// x64 ABI: structs larger than 8 bytes are passed by pointer to a copy.
func (h *Host) UseEmbed(tp ThreadParam, use bool) {
	h.procUseEmbed.Call(uintptr(unsafe.Pointer(&tp)), boolArg(use))
}

// IsUsingEmbed reports whether embedding is active for tp.
func (h *Host) IsUsingEmbed(tp ThreadParam) bool {
	r, _, _ := h.procCheckUsingEmbed.Call(uintptr(unsafe.Pointer(&tp)))
	return byte(r) != 0
}

// EmbedCallback delivers the translation for text. text must be the exact
// string received in OnEmbedText (it is hashed to name the wake-up event).
func (h *Host) EmbedCallback(tp ThreadParam, text, trans string) {
	t := utf16Ptr(text)
	tr := utf16Ptr(trans)
	h.procEmbedCallback.Call(
		uintptr(unsafe.Pointer(&tp)),
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(tr)),
	)
}

// InsertHookCode inserts a user hook code (H-code) into pid.
func (h *Host) InsertHookCode(pid uint32, code string) {
	h.procInsertHookCode.Call(uintptr(pid), uintptr(unsafe.Pointer(utf16Ptr(code))))
}

// RemoveHook removes the hook at addr.
func (h *Host) RemoveHook(pid uint32, addr uint64) {
	h.procRemoveHook.Call(uintptr(pid), uintptr(addr))
}

// InsertPCHooks inserts the generic GDI/D3D text hooks (which: 0 or 1).
func (h *Host) InsertPCHooks(pid uint32, which int32) {
	h.procInsertPCHooks.Call(uintptr(pid), uintptr(which))
}

func boolArg(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// utf16Ptr converts s to a NUL-terminated UTF-16 buffer; embedded NULs are dropped.
func utf16Ptr(s string) *uint16 {
	buf, err := windows.UTF16FromString(s)
	if err != nil {
		clean := make([]rune, 0, len(s))
		for _, r := range s {
			if r != 0 {
				clean = append(clean, r)
			}
		}
		buf, _ = windows.UTF16FromString(string(clean))
	}
	return &buf[0]
}
