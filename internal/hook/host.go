//go:build windows

package hook

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

const pipeBufSize = 64 * 1024

// Host manages named-pipe sessions with khutaf-hook64.dll injected into game processes.
type Host struct {
	runtimeDir string

	mu      sync.RWMutex
	handler Handler
	procs   map[uint32]*procSession
}

type hookMeta struct {
	code       string
	name       string
	embeddable bool
}

type pendingKey struct {
	tpKey string
	text  string
}

type procSession struct {
	host      *Host
	pid       uint32
	up        *overlappedPipe
	down      *overlappedPipe
	stopEvent windows.Handle
	connected atomic.Bool
	detached  atomic.Bool

	writeMu sync.Mutex

	mu           sync.Mutex
	hooks        map[uint64]hookMeta
	threads      map[string]HookInfo
	embedEnabled map[string]bool
	lastSettings *wireSettings
	pending      map[pendingKey][]uint64 // FIFO queue of reqIDs waiting for EmbedCallback

	wg sync.WaitGroup
}

// NewHost verifies runtimeDir contains khutaf-hook64.dll and initializes a pipe Host.
func NewHost(runtimeDir string) (*Host, error) {
	dllPath := filepath.Join(runtimeDir, nativeHookDLL)
	if _, err := os.Stat(dllPath); err != nil {
		return nil, fmt.Errorf("%s not found: %w", nativeHookDLL, err)
	}
	return &Host{
		runtimeDir: runtimeDir,
		procs:      map[uint32]*procSession{},
	}, nil
}

// RuntimeDir returns the folder holding the native hook binaries.
func (h *Host) RuntimeDir() string { return h.runtimeDir }

// Start registers the host event handler.
func (h *Host) Start(hd Handler) error {
	if hd == nil {
		return errors.New("nil handler")
	}
	h.mu.Lock()
	h.handler = hd
	h.mu.Unlock()
	return nil
}

func (h *Host) currentHandler() Handler {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.handler
}

// Settings is retained for compatibility with HostSettings.
func (h *Host) Settings(HostSettings) {}

// ConnectProcess creates the host-side named pipes for pid before DLL injection.
func (h *Host) ConnectProcess(pid uint32) {
	h.mu.Lock()
	if _, exists := h.procs[pid]; exists {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	stopEv, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		h.warn(fmt.Sprintf("CreateEvent(%d): %v", pid, err))
		return
	}
	up, err := createOverlappedPipe(
		fmt.Sprintf(`\\.\pipe\khutaf_%d_up`, pid),
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_OVERLAPPED,
		stopEv,
	)
	if err != nil {
		_ = windows.CloseHandle(stopEv)
		h.warn(fmt.Sprintf("create up pipe(%d): %v", pid, err))
		return
	}
	down, err := createOverlappedPipe(
		fmt.Sprintf(`\\.\pipe\khutaf_%d_down`, pid),
		windows.PIPE_ACCESS_OUTBOUND|windows.FILE_FLAG_OVERLAPPED,
		stopEv,
	)
	if err != nil {
		up.close()
		_ = windows.CloseHandle(stopEv)
		h.warn(fmt.Sprintf("create down pipe(%d): %v", pid, err))
		return
	}

	ps := &procSession{
		host:         h,
		pid:          pid,
		up:           up,
		down:         down,
		stopEvent:    stopEv,
		hooks:        map[uint64]hookMeta{},
		threads:      map[string]HookInfo{},
		embedEnabled: map[string]bool{},
		pending:      map[pendingKey][]uint64{},
	}

	h.mu.Lock()
	if _, exists := h.procs[pid]; exists {
		h.mu.Unlock()
		ps.closeHandles()
		return
	}
	h.procs[pid] = ps
	h.mu.Unlock()

	ps.wg.Add(1)
	go ps.serve()
}

// CheckIfNeedInject reports whether khutaf-hook64.dll is not yet loaded into pid.
func (h *Host) CheckIfNeedInject(pid uint32) bool {
	return !isModuleLoaded(pid, nativeHookDLL)
}

// DetachProcess signals the injected DLL in pid to unload and closes its pipes.
func (h *Host) DetachProcess(pid uint32) {
	h.mu.Lock()
	ps := h.procs[pid]
	if ps != nil {
		delete(h.procs, pid)
	}
	h.mu.Unlock()
	if ps == nil {
		return
	}

	ps.detached.Store(true)
	wasConnected := ps.connected.Load()
	if wasConnected {
		_ = ps.sendWithTimeout(&wireMsg{Kind: msgDetach}, 200*time.Millisecond)
	}
	_ = windows.SetEvent(ps.stopEvent)
	ps.wg.Wait()
	ps.closeHandles()

	if wasConnected {
		if hd := h.currentHandler(); hd != nil {
			hd.OnProcessRemove(pid)
		}
	}
}

// SettingsEx stores and pushes embed settings to pid.
func (h *Host) SettingsEx(pid uint32, s EmbedSettings) {
	ps := h.getProc(pid)
	if ps == nil {
		return
	}
	relative := s.FontSizeRelative
	if relative <= 0 {
		relative = 1
	}
	ws := wireSettings{
		WaitMs:      s.WaitTimeMs,
		DisplayMode: uint32(s.DisplayMode),
		FontFamily:  s.FontFamily,
		FontDir:     s.UnityFontDir,
		ClearText:       s.ClearText,
		ChangeSize:      s.ChangeFontSize,
		FontScale:       relative,
		StripTypewriter: s.StripTypewriterTags,
	}
	ps.mu.Lock()
	ps.lastSettings = &ws
	connected := ps.connected.Load()
	ps.mu.Unlock()

	if connected {
		_ = ps.send(&wireMsg{Kind: msgSettings, Settings: ws})
	}
}

// UseEmbed enables or disables embedded translation for a text thread.
func (h *Host) UseEmbed(tp ThreadParam, use bool) {
	ps := h.getProc(tp.ProcessID)
	if ps == nil {
		return
	}
	k := tp.Key()
	ps.mu.Lock()
	if use {
		ps.embedEnabled[k] = true
	} else {
		delete(ps.embedEnabled, k)
	}
	ps.mu.Unlock()

	if ps.connected.Load() {
		_ = ps.send(&wireMsg{
			Kind:   msgSetEmbed,
			HookID: tp.Addr,
			Ctx:    tp.Ctx,
			Ctx2:   tp.Ctx2,
			On:     use,
		})
	}
}

// IsUsingEmbed reports whether embedding is active for tp.
func (h *Host) IsUsingEmbed(tp ThreadParam) bool {
	ps := h.getProc(tp.ProcessID)
	if ps == nil {
		return false
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.embedEnabled[tp.Key()]
}

// EmbedCallback delivers the translation for an active EmbedRequest.
func (h *Host) EmbedCallback(tp ThreadParam, text, trans string) {
	ps := h.getProc(tp.ProcessID)
	if ps == nil {
		return
	}
	reqID, ok := ps.popPending(tp.Key(), text)
	if !ok {
		return
	}
	_ = ps.send(&wireMsg{
		Kind:    msgEmbedResponse,
		ReqID:   reqID,
		HasText: trans != "",
		Text:    trans,
	})
}

// InsertHookCode is unsupported by the native Unity hook backend.
func (h *Host) InsertHookCode(pid uint32, code string) {
	if hd := h.currentHandler(); hd != nil {
		hd.OnHostInfo(HostInfoWarning, "manual H-codes are not supported by the native Unity hook")
	}
}

func (h *Host) getProc(pid uint32) *procSession {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.procs[pid]
}

func (h *Host) warn(msg string) {
	if hd := h.currentHandler(); hd != nil {
		hd.OnHostInfo(HostInfoWarning, msg)
	}
}

func (ps *procSession) serve() {
	defer ps.wg.Done()

	downCh := make(chan error, 1)
	go func() { downCh <- ps.down.connect() }()

	upErr := ps.up.connect()
	downErr := <-downCh
	if upErr != nil || downErr != nil {
		ps.cleanupUnexpected()
		return
	}

	for {
		m, err := readMsg(ps.up)
		if err != nil {
			break
		}
		ps.dispatch(m)
	}
	ps.cleanupUnexpected()
}

func (ps *procSession) cleanupUnexpected() {
	if ps.detached.Load() {
		return
	}
	ps.host.mu.Lock()
	removed := ps.host.procs[ps.pid] == ps
	if removed {
		delete(ps.host.procs, ps.pid)
	}
	ps.host.mu.Unlock()

	if !removed {
		return
	}
	wasConnected := ps.connected.Load()
	_ = windows.SetEvent(ps.stopEvent)
	ps.closeHandles()
	if wasConnected {
		if hd := ps.host.currentHandler(); hd != nil {
			hd.OnProcessRemove(ps.pid)
		}
	}
}

func (ps *procSession) dispatch(m *wireMsg) {
	hd := ps.host.currentHandler()
	switch m.Kind {
	case msgHello:
		ps.mu.Lock()
		ws := ps.lastSettings
		ps.connected.Store(true)
		ps.mu.Unlock()
		if ws != nil {
			_ = ps.send(&wireMsg{Kind: msgSettings, Settings: *ws})
		}
		if hd != nil {
			hd.OnProcessConnect(ps.pid)
			if m.Engine != "" {
				hd.OnHostInfo(HostInfoEngineType, m.Engine)
				if m.UnityVersion != "" {
					hd.OnHostInfo(HostInfoConsole, "unity version: "+m.Engine+" ("+m.UnityVersion+")")
				}
			}
		}

	case msgHookAdded:
		ps.mu.Lock()
		ps.hooks[m.HookID] = hookMeta{code: m.Code, name: m.Name, embeddable: m.Embeddable}
		ps.mu.Unlock()
		if hd != nil {
			hd.OnHookInsert(ps.pid, m.HookID, m.Code)
		}

	case msgHookRemoved:
		ps.mu.Lock()
		var removed []HookInfo
		for k, info := range ps.threads {
			if info.Addr == m.HookID {
				removed = append(removed, info)
				delete(ps.threads, k)
			}
		}
		delete(ps.hooks, m.HookID)
		ps.mu.Unlock()
		if hd != nil {
			for _, info := range removed {
				hd.OnRemoveHook(info)
			}
		}

	case msgText:
		info := ps.ensureThread(m.HookID, m.Ctx, m.Ctx2, hd)
		if hd != nil {
			hd.OnOutput(info, m.Text)
		}

	case msgEmbedRequest:
		info := ps.ensureThread(m.HookID, m.Ctx, m.Ctx2, hd)
		ps.pushPending(info.Key, m.Text, m.ReqID)
		if hd != nil {
			hd.OnOutput(info, m.Text)
			hd.OnEmbedText(m.Text, info.TP)
		}

	case msgLog:
		if hd != nil {
			kind := HostInfoConsole
			if m.Level == logWarn || m.Level == logError {
				kind = HostInfoWarning
			}
			hd.OnHostInfo(kind, m.Text)
		}
	}
}

func (ps *procSession) ensureThread(hookID, ctx, ctx2 uint64, hd Handler) HookInfo {
	tp := ThreadParam{ProcessID: ps.pid, Addr: hookID, Ctx: ctx, Ctx2: ctx2}
	key := tp.Key()

	ps.mu.Lock()
	if info, ok := ps.threads[key]; ok {
		ps.mu.Unlock()
		return info
	}
	meta := ps.hooks[hookID]
	info := HookInfo{
		Key:        key,
		HookCode:   meta.code,
		HookName:   meta.name,
		TP:         tp,
		PID:        ps.pid,
		Addr:       hookID,
		Ctx:        ctx,
		Ctx2:       ctx2,
		Embeddable: meta.embeddable,
	}
	ps.threads[key] = info
	ps.mu.Unlock()

	if hd != nil {
		hd.OnNewHook(info)
	}
	return info
}

func (ps *procSession) pushPending(tpKey, text string, reqID uint64) {
	k := pendingKey{tpKey: tpKey, text: text}
	ps.mu.Lock()
	ps.pending[k] = append(ps.pending[k], reqID)
	ps.mu.Unlock()
}

func (ps *procSession) popPending(tpKey, text string) (uint64, bool) {
	k := pendingKey{tpKey: tpKey, text: text}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	q := ps.pending[k]
	if len(q) == 0 {
		return 0, false
	}
	id := q[0]
	if len(q) == 1 {
		delete(ps.pending, k)
	} else {
		ps.pending[k] = q[1:]
	}
	return id, true
}

func (ps *procSession) send(m *wireMsg) error {
	return ps.sendWithTimeout(m, 5*time.Second)
}

func (ps *procSession) sendWithTimeout(m *wireMsg, timeout time.Duration) error {
	ps.writeMu.Lock()
	defer ps.writeMu.Unlock()
	if ps.down == nil {
		return errors.New("pipe closed")
	}
	return ps.down.writeAll(m.encode(), timeout)
}

func (ps *procSession) closeHandles() {
	if ps.up != nil {
		ps.up.close()
	}
	if ps.down != nil {
		ps.down.close()
	}
	if ps.stopEvent != 0 {
		_ = windows.CloseHandle(ps.stopEvent)
		ps.stopEvent = 0
	}
}

// ---- overlapped Windows named pipe wrapper ----

type overlappedPipe struct {
	h         windows.Handle
	ioEvent   windows.Handle
	stopEvent windows.Handle
}

func createOverlappedPipe(name string, openMode uint32, stopEvent windows.Handle) (*overlappedPipe, error) {
	wname, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateNamedPipe(
		wname,
		openMode,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1,
		pipeBufSize,
		pipeBufSize,
		0,
		nil,
	)
	if err != nil {
		return nil, err
	}
	ioEv, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	return &overlappedPipe{h: h, ioEvent: ioEv, stopEvent: stopEvent}, nil
}

func (p *overlappedPipe) connect() error {
	if err := windows.ResetEvent(p.ioEvent); err != nil {
		return err
	}
	var ov windows.Overlapped
	ov.HEvent = p.ioEvent
	err := windows.ConnectNamedPipe(p.h, &ov)
	if err == nil || errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		return nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		return err
	}
	idx, waitErr := windows.WaitForMultipleObjects([]windows.Handle{p.ioEvent, p.stopEvent}, false, windows.INFINITE)
	if waitErr != nil || idx != windows.WAIT_OBJECT_0 {
		_ = windows.CancelIoEx(p.h, &ov)
		var dummy uint32
		_ = windows.GetOverlappedResult(p.h, &ov, &dummy, true)
		if waitErr != nil {
			return waitErr
		}
		return errors.New("pipe closed")
	}
	var dummy uint32
	return windows.GetOverlappedResult(p.h, &ov, &dummy, false)
}

func (p *overlappedPipe) Read(buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, nil
	}
	if err := windows.ResetEvent(p.ioEvent); err != nil {
		return 0, err
	}
	var ov windows.Overlapped
	ov.HEvent = p.ioEvent
	var n uint32
	err := windows.ReadFile(p.h, buf, &n, &ov)
	if err == nil {
		return int(n), nil
	}
	if !errors.Is(err, windows.ERROR_IO_PENDING) {
		if isPipeEOF(err) {
			return 0, io.EOF
		}
		return 0, err
	}
	idx, waitErr := windows.WaitForMultipleObjects([]windows.Handle{p.ioEvent, p.stopEvent}, false, windows.INFINITE)
	if waitErr != nil || idx != windows.WAIT_OBJECT_0 {
		_ = windows.CancelIoEx(p.h, &ov)
		_ = windows.GetOverlappedResult(p.h, &ov, &n, true)
		if waitErr != nil {
			return 0, waitErr
		}
		return 0, io.EOF
	}
	if err := windows.GetOverlappedResult(p.h, &ov, &n, false); err != nil {
		if isPipeEOF(err) {
			return 0, io.EOF
		}
		return 0, err
	}
	return int(n), nil
}

func (p *overlappedPipe) writeAll(buf []byte, timeout time.Duration) error {
	timeoutMs := uint32(timeout.Milliseconds())
	if timeoutMs == 0 {
		timeoutMs = 1
	}
	for len(buf) > 0 {
		if err := windows.ResetEvent(p.ioEvent); err != nil {
			return err
		}
		var ov windows.Overlapped
		ov.HEvent = p.ioEvent
		var n uint32
		err := windows.WriteFile(p.h, buf, &n, &ov)
		if err == nil {
			buf = buf[n:]
			continue
		}
		if !errors.Is(err, windows.ERROR_IO_PENDING) {
			return err
		}
		idx, waitErr := windows.WaitForMultipleObjects([]windows.Handle{p.ioEvent, p.stopEvent}, false, timeoutMs)
		if waitErr != nil || idx != windows.WAIT_OBJECT_0 {
			_ = windows.CancelIoEx(p.h, &ov)
			_ = windows.GetOverlappedResult(p.h, &ov, &n, true)
			if waitErr != nil {
				return waitErr
			}
			return errors.New("pipe write aborted or timed out")
		}
		if err := windows.GetOverlappedResult(p.h, &ov, &n, false); err != nil {
			return err
		}
		buf = buf[n:]
	}
	return nil
}

func (p *overlappedPipe) close() {
	if p.h != 0 && p.h != windows.InvalidHandle {
		_ = windows.DisconnectNamedPipe(p.h)
		_ = windows.CloseHandle(p.h)
		p.h = 0
	}
	if p.ioEvent != 0 {
		_ = windows.CloseHandle(p.ioEvent)
		p.ioEvent = 0
	}
}

func isPipeEOF(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) ||
		errors.Is(err, windows.ERROR_HANDLE_EOF) ||
		errors.Is(err, windows.ERROR_OPERATION_ABORTED)
}
