//go:build windows

package hotkeys

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"khutaf/internal/config"
)

// Action identifies an in-game shortcut action.
type Action string

const (
	ActionCycleMode       Action = "cycleMode"
	ActionTogglePause     Action = "togglePause"
	ActionRetranslate     Action = "retranslate"
	ActionFontScaleUp     Action = "fontScaleUp"
	ActionFontScaleDown   Action = "fontScaleDown"
	ActionToggleHooksMenu Action = "toggleHooksMenu"
)

const (
	modAlt      uint32 = 0x0001
	modControl  uint32 = 0x0002
	modShift    uint32 = 0x0004
	modWin      uint32 = 0x0008
	modNoRepeat uint32 = 0x4000

	wmQuit        uint32 = 0x0012
	wmHotkey      uint32 = 0x0312
	wmReconfigure uint32 = 0x8001 // WM_APP + 1
	pmNoRemove    uint32 = 0x0001
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procRegisterHotKey     = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey   = user32.NewProc("UnregisterHotKey")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procPeekMessageW       = user32.NewProc("PeekMessageW")
	procPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
)

type point struct {
	X int32
	Y int32
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

var namedVK = map[string]uint32{
	"BACKSPACE": 0x08,
	"TAB":       0x09,
	"ENTER":     0x0D,
	"RETURN":    0x0D,
	"SPACE":     0x20,
	"PAGEUP":    0x21,
	"PGUP":      0x21,
	"PAGEDOWN":  0x22,
	"PGDN":      0x22,
	"END":       0x23,
	"HOME":      0x24,
	"LEFT":      0x25,
	"UP":        0x26,
	"RIGHT":     0x27,
	"DOWN":      0x28,
	"INSERT":    0x2D,
	"INS":       0x2D,
	"DELETE":    0x2E,
	"DEL":       0x2E,
	"PLUS":      0xBB,
	"EQUAL":     0xBB,
	"=":         0xBB,
	"MINUS":     0xBD,
	"-":         0xBD,
	",":         0xBC,
	".":         0xBE,
	"/":         0xBF,
	";":         0xBA,
	"'":         0xDE,
	"[":         0xDB,
	"\\":        0xDC,
	"]":         0xDD,
	"`":         0xC0,
}

// ParseCombo parses a human-readable shortcut such as "Ctrl+Alt+M" into Win32 modifiers and virtual key.
// At least one modifier is required unless the key is a function key (F1..F24).
func ParseCombo(combo string) (mods uint32, vk uint32, ok bool) {
	trimmed := strings.TrimSpace(combo)
	if trimmed == "" {
		return 0, 0, false
	}
	parts := strings.Split(trimmed, "+")
	var keyToken string
	for _, raw := range parts {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return 0, 0, false
		}
		upper := strings.ToUpper(tok)
		switch upper {
		case "CTRL", "CONTROL":
			mods |= modControl
		case "ALT":
			mods |= modAlt
		case "SHIFT":
			mods |= modShift
		case "WIN", "SUPER", "META":
			mods |= modWin
		default:
			if keyToken != "" {
				return 0, 0, false
			}
			keyToken = upper
		}
	}
	if keyToken == "" {
		return 0, 0, false
	}
	vk, ok = parseVirtualKey(keyToken)
	if !ok {
		return 0, 0, false
	}
	isFnKey := vk >= 0x70 && vk <= 0x87
	if mods == 0 && !isFnKey {
		return 0, 0, false
	}
	return mods, vk, true
}

func parseVirtualKey(token string) (uint32, bool) {
	if len(token) == 1 {
		b := token[0]
		if (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') {
			return uint32(b), true
		}
	}
	if strings.HasPrefix(token, "F") && len(token) >= 2 {
		if n, err := strconv.Atoi(token[1:]); err == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), true
		}
	}
	if vk, exists := namedVK[token]; exists {
		return vk, true
	}
	return 0, false
}

// BindingsFromConfig extracts active hotkey bindings from config.
// When cfg.Enabled is false, an empty map is returned so all hotkeys are unregistered.
func BindingsFromConfig(cfg config.HotkeySettings) map[Action]string {
	if !cfg.Enabled {
		return map[Action]string{}
	}
	pairs := []struct {
		act Action
		val string
	}{
		{ActionCycleMode, cfg.CycleMode},
		{ActionTogglePause, cfg.TogglePause},
		{ActionRetranslate, cfg.Retranslate},
		{ActionFontScaleUp, cfg.FontScaleUp},
		{ActionFontScaleDown, cfg.FontScaleDown},
		{ActionToggleHooksMenu, cfg.ToggleHooksMenu},
	}
	out := make(map[Action]string, len(pairs))
	for _, p := range pairs {
		if v := strings.TrimSpace(p.val); v != "" {
			out[p.act] = v
		}
	}
	return out
}

// Manager owns a dedicated Win32 message-loop thread for RegisterHotKey bindings.
type Manager struct {
	onTrigger    func(Action)
	registerFn   func(id int, mods, vk uint32) error
	unregisterFn func(id int)

	mu        sync.Mutex
	tid       uint32
	closed    bool
	desired   map[Action]string
	reconfAck chan []string

	done chan struct{}
}

// NewManager starts the background hotkey message loop.
func NewManager(onTrigger func(Action)) *Manager {
	m := &Manager{
		onTrigger:    onTrigger,
		registerFn:   winRegisterHotKey,
		unregisterFn: winUnregisterHotKey,
		desired:      map[Action]string{},
		done:         make(chan struct{}),
	}
	ready := make(chan struct{})
	go m.runLoop(ready)
	<-ready
	return m
}

func winRegisterHotKey(id int, mods, vk uint32) error {
	ret, _, callErr := procRegisterHotKey.Call(0, uintptr(id), uintptr(mods|modNoRepeat), uintptr(vk))
	if ret == 0 {
		var errno syscall.Errno
		if callErr != nil {
			errno, _ = callErr.(syscall.Errno)
		}
		return errno
	}
	return nil
}

func winUnregisterHotKey(id int) {
	_, _, _ = procUnregisterHotKey.Call(0, uintptr(id))
}

// Configure replaces all registered global hotkeys with bindings.
// It returns a non-nil error listing any bindings that failed syntax validation or Win32 registration.
func (m *Manager) Configure(bindings map[Action]string) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	cp := make(map[Action]string, len(bindings))
	for k, v := range bindings {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			cp[k] = trimmed
		}
	}
	ack := make(chan []string, 1)
	m.desired = cp
	m.reconfAck = ack
	tid := m.tid
	m.mu.Unlock()

	ret, _, err := procPostThreadMessageW.Call(uintptr(tid), uintptr(wmReconfigure), 0, 0)
	if ret == 0 {
		m.mu.Lock()
		if m.reconfAck == ack {
			m.reconfAck = nil
		}
		m.mu.Unlock()
		return fmt.Errorf("post hotkey configure: %v", err)
	}

	warnings := <-ack
	if len(warnings) > 0 {
		return fmt.Errorf("hotkey warnings: %s", strings.Join(warnings, "; "))
	}
	return nil
}

// Close unregisters all global hotkeys and stops the Win32 message loop.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	tid := m.tid
	m.mu.Unlock()

	_, _, _ = procPostThreadMessageW.Call(uintptr(tid), uintptr(wmQuit), 0, 0)
	<-m.done
}

func (m *Manager) runLoop(ready chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.done)

	// Force Windows to create the message queue before publishing tid.
	var msg winMsg
	_, _, _ = procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, uintptr(pmNoRemove))
	tidRet, _, _ := procGetCurrentThreadID.Call()

	m.mu.Lock()
	m.tid = uint32(tidRet)
	m.mu.Unlock()
	close(ready)

	idToAction := map[int]Action{}
	unregisterAll := func() {
		for id := range idToAction {
			m.unregisterFn(id)
		}
		idToAction = map[int]Action{}
	}
	applyDesired := func() {
		unregisterAll()
		m.mu.Lock()
		cp := make(map[Action]string, len(m.desired))
		for k, v := range m.desired {
			cp[k] = v
		}
		ack := m.reconfAck
		m.reconfAck = nil
		m.mu.Unlock()

		acts := make([]Action, 0, len(cp))
		for act := range cp {
			acts = append(acts, act)
		}
		sort.Slice(acts, func(i, j int) bool { return acts[i] < acts[j] })

		var warnings []string
		nextID := 1
		for _, act := range acts {
			combo := cp[act]
			mods, vk, ok := ParseCombo(combo)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("%s (%q invalid)", act, combo))
				continue
			}
			id := nextID
			nextID++
			if err := m.registerFn(id, mods, vk); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s (%s unavailable: %v)", act, combo, err))
				continue
			}
			idToAction[id] = act
		}
		if ack != nil {
			ack <- warnings
		}
	}

	defer func() {
		unregisterAll()
		m.mu.Lock()
		ack := m.reconfAck
		m.reconfAck = nil
		m.mu.Unlock()
		if ack != nil {
			ack <- nil
		}
	}()

	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		switch msg.Message {
		case wmReconfigure:
			applyDesired()
		case wmHotkey:
			id := int(msg.WParam)
			if act, ok := idToAction[id]; ok && m.onTrigger != nil {
				go m.onTrigger(act)
			}
		}
	}
}
