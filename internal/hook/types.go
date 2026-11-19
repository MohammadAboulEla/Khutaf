// Package hook provides the pure-Go overlapped named-pipe host for khutaf-hook64.dll,
// Win32 process discovery, and CreateRemoteThread(LoadLibraryW) DLL injection.
package hook

import "fmt"

// ThreadParam matches the C struct ThreadParam (24 bytes, 8-byte aligned).
type ThreadParam struct {
	ProcessID uint32
	_         uint32 // padding so Addr is 8-byte aligned
	Addr      uint64
	Ctx       uint64
	Ctx2      uint64
}

// Key is a stable string ID for the UI.
func (tp ThreadParam) Key() string {
	return fmt.Sprintf("%d:%x:%x:%x", tp.ProcessID, tp.Addr, tp.Ctx, tp.Ctx2)
}

// HostInfo mirrors the HOSTINFO enum in const.h.
type HostInfo int32

const (
	HostInfoConsole HostInfo = iota
	HostInfoWarning
	HostInfoEmuWarning
	HostInfoEmuConnected
	HostInfoEngineType
)

// DisplayMode mirrors the Displaymode enum in types.h.
type DisplayMode uint32

const (
	DisplayTrans DisplayMode = iota
	DisplayOriTrans
	DisplayTransOri
	DisplayOri
)

// EmbedSettings is pushed into the game's hook DLL over named-pipe IPC.
type EmbedSettings struct {
	WaitTimeMs          uint32
	FontFamily          string // empty = keep game font
	DisplayMode         DisplayMode
	ClearText           bool
	ChangeFontSize      bool
	FontSizeRelative    float32
	UnityFontDir        string // folder with arialuni_sdf_u* bundles; empty disables
	StripTypewriterTags bool
	NonBlocking         bool
	PendingText         string // shown immediately while async translation runs; empty keeps original
}

// HookInfo describes a discovered text thread.
type HookInfo struct {
	Key        string      `json:"key"`
	HookCode   string      `json:"hookCode"`
	HookName   string      `json:"hookName"`
	TP         ThreadParam `json:"-"`
	PID        uint32      `json:"pid"`
	Addr       uint64      `json:"addr"`
	Ctx        uint64      `json:"ctx"`
	Ctx2       uint64      `json:"ctx2"`
	Embeddable bool        `json:"embeddable"`
}

// Handler receives host events. All methods are called from native threads and
// must return quickly; do heavy work in a goroutine.
type Handler interface {
	OnProcessConnect(pid uint32)
	OnProcessRemove(pid uint32)
	OnNewHook(h HookInfo)
	OnRemoveHook(h HookInfo)
	OnOutput(h HookInfo, text string)
	OnHostInfo(kind HostInfo, msg string)
	OnHookInsert(pid uint32, addr uint64, hookCode string)
	// OnEmbedText is invoked while the game's text function waits (up to WaitTimeMs)
	// for Host.EmbedCallback(tp, text, translation).
	OnEmbedText(text string, tp ThreadParam)
}
