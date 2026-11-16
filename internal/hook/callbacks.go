//go:build windows

package hook

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Native callbacks registered with Luna_Start. syscall.NewCallback slots are
// limited and never freed, so they are created once at package init.
var (
	cbProcessConnect = syscall.NewCallback(onProcessConnect)
	cbProcessRemove  = syscall.NewCallback(onProcessRemove)
	cbNewHook        = syscall.NewCallback(onNewHook)
	cbRemoveHook     = syscall.NewCallback(onRemoveHook)
	cbOutput         = syscall.NewCallback(onOutput)
	cbHostInfo       = syscall.NewCallback(onHostInfo)
	cbHookInsert     = syscall.NewCallback(onHookInsert)
	cbEmbed          = syscall.NewCallback(onEmbed)
	cbI18NQuery      = syscall.NewCallback(onI18NQuery)
	cbEmuGameInfo    = syscall.NewCallback(onEmuGameInfo)
)

func currentHandler() Handler {
	handlerMu.RLock()
	defer handlerMu.RUnlock()
	return handler
}

func wstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(p)))
}

func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.BytePtrToString((*byte)(unsafe.Pointer(p)))
}

// readTP copies a by-value ThreadParam that the x64 ABI passed as a pointer.
func readTP(p uintptr) ThreadParam {
	if p == 0 {
		return ThreadParam{}
	}
	return *(*ThreadParam)(unsafe.Pointer(p))
}

func makeHookInfo(hookCode, hookName uintptr, tpPtr uintptr) HookInfo {
	tp := readTP(tpPtr)
	return HookInfo{
		Key:      tp.Key(),
		HookCode: wstr(hookCode),
		HookName: cstr(hookName),
		TP:       tp,
		PID:      tp.ProcessID,
		Addr:     tp.Addr,
		Ctx:      tp.Ctx,
		Ctx2:     tp.Ctx2,
	}
}

func onProcessConnect(pid uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnProcessConnect(uint32(pid))
	}
	return 0
}

func onProcessRemove(pid uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnProcessRemove(uint32(pid))
	}
	return 0
}

func onNewHook(hookCode, hookName, tp, isEmbeddable uintptr) uintptr {
	if h := currentHandler(); h != nil {
		info := makeHookInfo(hookCode, hookName, tp)
		info.Embeddable = byte(isEmbeddable) != 0
		h.OnNewHook(info)
	}
	return 0
}

func onRemoveHook(hookCode, hookName, tp uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnRemoveHook(makeHookInfo(hookCode, hookName, tp))
	}
	return 0
}

func onOutput(hookCode, hookName, tp, text uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnOutput(makeHookInfo(hookCode, hookName, tp), wstr(text))
	}
	return 0
}

func onHostInfo(kind, msg uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnHostInfo(HostInfo(int32(kind)), wstr(msg))
	}
	return 0
}

func onHookInsert(pid, addr, hookCode uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnHookInsert(uint32(pid), uint64(addr), wstr(hookCode))
	}
	return 0
}

func onEmbed(text, tp uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnEmbedText(wstr(text), readTP(tp))
	}
	return 0
}

// onI18NQuery returns NULL so the host keeps its built-in strings
// (a non-NULL result would be freed by the host with `delete`).
func onI18NQuery(text uintptr) uintptr { return 0 }

func onEmuGameInfo(id, title, version uintptr) uintptr {
	if h := currentHandler(); h != nil {
		h.OnHostInfo(HostInfoConsole, "[Emu] "+wstr(id)+" "+wstr(title)+" "+wstr(version))
	}
	return 0
}
