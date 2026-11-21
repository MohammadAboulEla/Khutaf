//go:build windows

package main

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmDestroy     uint32 = 0x0002
	wmNull        uint32 = 0x0000
	wmQuit        uint32 = 0x0012
	wmLButtonUp   uint32 = 0x0202
	wmLButtonDbl  uint32 = 0x0203
	wmRButtonUp   uint32 = 0x0205
	wmContextMenu uint32 = 0x007B
	wmTrayIcon    uint32 = 0x8002 // WM_APP + 2

	nimAdd    uint32 = 0x00000000
	nimDelete uint32 = 0x00000002

	nifMessage uint32 = 0x00000001
	nifIcon    uint32 = 0x00000002
	nifTip     uint32 = 0x00000004

	mfString    uintptr = 0x00000000
	mfGrayed    uintptr = 0x00000001
	mfSeparator uintptr = 0x00000800

	tpmReturnCmd   uintptr = 0x0100
	tpmNoNotify    uintptr = 0x0080
	tpmRightButton uintptr = 0x0002

	idiApplication uintptr = 32512

	trayMenuStatus     = 1001
	trayMenuShowHide   = 1002
	trayMenuPause      = 1003
	trayMenuQuickHooks = 1004
	trayMenuDetach     = 1005
	trayMenuQuit       = 1006
)

var (
	trayUser32   = windows.NewLazySystemDLL("user32.dll")
	trayKernel32 = windows.NewLazySystemDLL("kernel32.dll")
	trayShell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassExW     = trayUser32.NewProc("RegisterClassExW")
	procCreateWindowExW      = trayUser32.NewProc("CreateWindowExW")
	procDestroyWindow        = trayUser32.NewProc("DestroyWindow")
	procDefWindowProcW       = trayUser32.NewProc("DefWindowProcW")
	procPostQuitMessage      = trayUser32.NewProc("PostQuitMessage")
	procPostMessageW         = trayUser32.NewProc("PostMessageW")
	procGetMessageW          = trayUser32.NewProc("GetMessageW")
	procTranslateMessage     = trayUser32.NewProc("TranslateMessage")
	procDispatchMessageW     = trayUser32.NewProc("DispatchMessageW")
	procCreatePopupMenu      = trayUser32.NewProc("CreatePopupMenu")
	procDestroyMenu          = trayUser32.NewProc("DestroyMenu")
	procAppendMenuW          = trayUser32.NewProc("AppendMenuW")
	procTrackPopupMenu       = trayUser32.NewProc("TrackPopupMenu")
	procGetCursorPos         = trayUser32.NewProc("GetCursorPos")
	procSetForegroundWindow  = trayUser32.NewProc("SetForegroundWindow")
	procLoadIconW            = trayUser32.NewProc("LoadIconW")
	procDestroyIcon          = trayUser32.NewProc("DestroyIcon")
	procRegisterWindowMsgW   = trayUser32.NewProc("RegisterWindowMessageW")
	procGetModuleHandleW     = trayKernel32.NewProc("GetModuleHandleW")
	procShellNotifyIconW     = trayShell32.NewProc("Shell_NotifyIconW")
	procExtractIconExW       = trayShell32.NewProc("ExtractIconExW")
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type trayPoint struct {
	X int32
	Y int32
}

type trayMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      trayPoint
}

type notifyIconDataW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// TrayState describes dynamic menu labels and enabled states shown in the system tray menu.
type TrayState struct {
	Attached bool
	GameName string
	Paused   bool
	Lang     string
	Hidden   bool
}

// TrayCallbacks wires tray interactions into the Wails application.
type TrayCallbacks struct {
	State      func() TrayState
	ToggleShow func()
	TogglePause func()
	OpenHooks  func()
	Detach     func()
	Quit       func()
}

type trayManager struct {
	cb               TrayCallbacks
	hwnd             uintptr
	hicon            uintptr
	ownsIcon         bool
	wmTaskbarCreated uint32

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

var (
	activeTrayMu sync.Mutex
	activeTray   *trayManager
	trayWndProc  = syscall.NewCallback(handleTrayWndProc)
)

func startTrayManager(cb TrayCallbacks) *trayManager {
	t := &trayManager{
		cb:   cb,
		done: make(chan struct{}),
	}
	ready := make(chan struct{})
	go t.run(ready)
	<-ready
	return t
}

func (t *trayManager) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	hwnd := t.hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		_, _, _ = procPostMessageW.Call(hwnd, uintptr(wmDestroy), 0, 0)
	}
	<-t.done
}

func (t *trayManager) run(ready chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)

	hInst, _, _ := procGetModuleHandleW.Call(0)
	clsName, _ := syscall.UTF16PtrFromString("KhutafTrayWindowClass")
	wndTitle, _ := syscall.UTF16PtrFromString("Khutaf Tray")

	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   trayWndProc,
		HInstance:     hInst,
		LpszClassName: clsName,
	}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	t.loadTrayIcon()
	tbMsgName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	tbRet, _, _ := procRegisterWindowMsgW.Call(uintptr(unsafe.Pointer(tbMsgName)))
	t.wmTaskbarCreated = uint32(tbRet)

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(wndTitle)),
		0, 0, 0, 0, 0,
		0, 0, hInst, 0,
	)

	activeTrayMu.Lock()
	activeTray = t
	activeTrayMu.Unlock()

	t.mu.Lock()
	t.hwnd = hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		t.addIcon()
	}
	close(ready)

	var msg trayMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	t.removeIcon()
	if t.ownsIcon && t.hicon != 0 {
		_, _, _ = procDestroyIcon.Call(t.hicon)
	}
	activeTrayMu.Lock()
	if activeTray == t {
		activeTray = nil
	}
	activeTrayMu.Unlock()
}

func (t *trayManager) loadTrayIcon() {
	if exe, err := os.Executable(); err == nil {
		if exePtr, perr := syscall.UTF16PtrFromString(exe); perr == nil {
			var small uintptr
			cnt, _, _ := procExtractIconExW.Call(
				uintptr(unsafe.Pointer(exePtr)),
				0,
				0,
				uintptr(unsafe.Pointer(&small)),
				1,
			)
			if cnt > 0 && small != 0 {
				t.hicon = small
				t.ownsIcon = true
				return
			}
		}
	}
	hicon, _, _ := procLoadIconW.Call(0, idiApplication)
	t.hicon = hicon
}

func (t *trayManager) makeNID() notifyIconDataW {
	var nid notifyIconDataW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = 1
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTrayIcon
	nid.HIcon = t.hicon
	if tip, err := syscall.UTF16FromString("Khutaf — خُطّاف"); err == nil {
		copy(nid.SzTip[:], tip)
	}
	return nid
}

func (t *trayManager) addIcon() {
	if t.hwnd == 0 {
		return
	}
	nid := t.makeNID()
	_, _, _ = procShellNotifyIconW.Call(uintptr(nimAdd), uintptr(unsafe.Pointer(&nid)))
}

func (t *trayManager) removeIcon() {
	if t.hwnd == 0 {
		return
	}
	nid := notifyIconDataW{
		CbSize: uint32(unsafe.Sizeof(notifyIconDataW{})),
		HWnd:   t.hwnd,
		UID:    1,
	}
	_, _, _ = procShellNotifyIconW.Call(uintptr(nimDelete), uintptr(unsafe.Pointer(&nid)))
}

func handleTrayWndProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	activeTrayMu.Lock()
	t := activeTray
	activeTrayMu.Unlock()

	if t != nil {
		if t.wmTaskbarCreated != 0 && msg == t.wmTaskbarCreated {
			t.addIcon()
			return 0
		}
		switch msg {
		case wmTrayIcon:
			ev := uint32(lparam & 0xFFFF)
			switch ev {
			case wmLButtonDbl, wmLButtonUp:
				if t.cb.ToggleShow != nil {
					go t.cb.ToggleShow()
				}
			case wmRButtonUp, wmContextMenu:
				t.showContextMenu()
			}
			return 0
		case wmDestroy:
			_, _, _ = procPostQuitMessage.Call(0)
			return 0
		}
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func appendTrayItem(menu uintptr, flags uintptr, id uintptr, text string) {
	ptr, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	_, _, _ = procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(ptr)))
}

func (t *trayManager) showContextMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	var st TrayState
	if t.cb.State != nil {
		st = t.cb.State()
	}
	ar := st.Lang != "en"

	statusText := "Khutaf · Idle"
	if ar {
		statusText = "خُطّاف · في الانتظار"
	}
	if st.Attached {
		if ar {
			statusText = "متصل: " + st.GameName
		} else {
			statusText = "Attached: " + st.GameName
		}
		if st.Paused {
			if ar {
				statusText += " (متوقف مؤقتاً)"
			} else {
				statusText += " (Paused)"
			}
		}
	}

	showLabel := "Show / Hide Window"
	pauseLabel := "Pause Translation"
	if st.Paused {
		pauseLabel = "Resume Translation"
	}
	hooksLabel := "Quick Hooks Picker..."
	detachLabel := "Detach Game"
	quitLabel := "Quit Khutaf"
	if ar {
		if st.Hidden {
			showLabel = "إظهار خُطّاف"
		} else {
			showLabel = "إخفاء إلى شريط المهام"
		}
		if st.Paused {
			pauseLabel = "استئناف الترجمة"
		} else {
			pauseLabel = "إيقاف الترجمة مؤقتاً"
		}
		hooksLabel = "قنوات النصوص (Hooks)..."
		detachLabel = "فصل اللعبة"
		quitLabel = "إغلاق خُطّاف نهائياً"
	} else if st.Hidden {
		showLabel = "Show Khutaf"
	} else {
		showLabel = "Hide to Tray"
	}

	appendTrayItem(menu, mfString|mfGrayed, trayMenuStatus, statusText)
	_, _, _ = procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendTrayItem(menu, mfString, trayMenuShowHide, showLabel)

	attachedFlags := mfString
	if !st.Attached {
		attachedFlags |= mfGrayed
	}
	appendTrayItem(menu, attachedFlags, trayMenuPause, pauseLabel)
	appendTrayItem(menu, attachedFlags, trayMenuQuickHooks, hooksLabel)
	appendTrayItem(menu, attachedFlags, trayMenuDetach, detachLabel)
	_, _, _ = procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	appendTrayItem(menu, mfString, trayMenuQuit, quitLabel)

	var pt trayPoint
	_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	_, _, _ = procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmReturnCmd|tpmNoNotify|tpmRightButton,
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		t.hwnd,
		0,
	)
	_, _, _ = procPostMessageW.Call(t.hwnd, uintptr(wmNull), 0, 0)

	switch int(cmd) {
	case trayMenuShowHide:
		if t.cb.ToggleShow != nil {
			go t.cb.ToggleShow()
		}
	case trayMenuPause:
		if t.cb.TogglePause != nil {
			go t.cb.TogglePause()
		}
	case trayMenuQuickHooks:
		if t.cb.OpenHooks != nil {
			go t.cb.OpenHooks()
		}
	case trayMenuDetach:
		if t.cb.Detach != nil {
			go t.cb.Detach()
		}
	case trayMenuQuit:
		if t.cb.Quit != nil {
			go t.cb.Quit()
		}
	}
}
