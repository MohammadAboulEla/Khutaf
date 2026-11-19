//go:build windows

package hook

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	pipeBufSize         = 64 * 1024
	handshakeVersion    = uint32(1)
	handshakeNonceBytes = 16
	handshakeSize       = 64
)

var (
	handshakeMagic       = [4]byte{'K', 'H', 'U', 'T'}
	procOpenFileMappingW = kernel32.NewProc("OpenFileMappingW")
)

// TelemetryFunc receives structured telemetry events from the hook host.
type TelemetryFunc func(subsys, fnName string, durUs int64, level, msg string, fields map[string]any)

// Host manages named-pipe sessions with khutaf-hook64.dll injected into game processes.
type Host struct {
	runtimeDir string

	mu      sync.RWMutex
	handler Handler
	procs   map[uint32]*procSession
	telem   atomic.Pointer[TelemetryFunc]
}

// SetTelemetry registers an optional telemetry callback.
func (h *Host) SetTelemetry(fn TelemetryFunc) {
	if h == nil {
		return
	}
	if fn == nil {
		h.telem.Store(nil)
		return
	}
	h.telem.Store(&fn)
}

func (h *Host) emitTelem(fnName string, durUs int64, level, msg string, fields map[string]any) {
	if h == nil {
		return
	}
	if p := h.telem.Load(); p != nil && *p != nil {
		(*p)("IPC_PIPE", fnName, durUs, level, msg, fields)
	}
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

type pendingEntry struct {
	id uint64
	at time.Time
}

type procSession struct {
	host      *Host
	pid       uint32
	up        *overlappedPipe
	down      *overlappedPipe
	stopEvent windows.Handle
	shmMu     sync.Mutex
	shmHandle windows.Handle
	shmAddr   uintptr
	connected atomic.Bool
	detached  atomic.Bool
	helloCh   chan struct{}
	helloOnce sync.Once

	writeMu sync.Mutex

	mu           sync.Mutex
	hooks        map[uint64]hookMeta
	threads      map[string]HookInfo
	embedEnabled map[string]bool
	lastSettings *wireSettings
	pending      map[pendingKey][]pendingEntry // FIFO queue of reqIDs waiting for EmbedCallback

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

// ConnectProcess creates the host-side named pipes for pid before DLL injection.
func (h *Host) ConnectProcess(pid uint32) {
	h.mu.Lock()
	if _, exists := h.procs[pid]; exists {
		h.mu.Unlock()
		return
	}
	h.mu.Unlock()

	nonceBuf := make([]byte, handshakeNonceBytes)
	if _, err := rand.Read(nonceBuf); err != nil {
		h.warn(fmt.Sprintf("nonce(%d): %v", pid, err))
		return
	}
	nonce := hex.EncodeToString(nonceBuf)

	sd, err := ownerOnlySecurityDescriptor()
	if err != nil {
		h.warn(fmt.Sprintf("security descriptor(%d): %v", pid, err))
		return
	}

	stopEv, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		h.warn(fmt.Sprintf("CreateEvent(%d): %v", pid, err))
		return
	}
	up, err := createOverlappedPipe(
		fmt.Sprintf(`\\.\pipe\khutaf_%d_%s_up`, pid, nonce),
		windows.PIPE_ACCESS_INBOUND|windows.FILE_FLAG_OVERLAPPED,
		stopEv,
		sd,
	)
	if err != nil {
		_ = windows.CloseHandle(stopEv)
		h.warn(fmt.Sprintf("create up pipe(%d): %v", pid, err))
		return
	}
	down, err := createOverlappedPipe(
		fmt.Sprintf(`\\.\pipe\khutaf_%d_%s_down`, pid, nonce),
		windows.PIPE_ACCESS_OUTBOUND|windows.FILE_FLAG_OVERLAPPED,
		stopEv,
		sd,
	)
	if err != nil {
		up.close()
		_ = windows.CloseHandle(stopEv)
		h.warn(fmt.Sprintf("create down pipe(%d): %v", pid, err))
		return
	}
	shmHandle, shmAddr, err := createHandshakeMapping(pid, nonce, sd)
	if err != nil {
		down.close()
		up.close()
		_ = windows.CloseHandle(stopEv)
		h.warn(fmt.Sprintf("create handshake(%d): %v", pid, err))
		return
	}

	ps := &procSession{
		host:      h,
		pid:       pid,
		up:        up,
		down:      down,
		stopEvent: stopEv,
		shmHandle: shmHandle,
		shmAddr:   shmAddr,
		helloCh:   make(chan struct{}),
		hooks:     map[uint64]hookMeta{},
		threads:   map[string]HookInfo{},
		embedEnabled: map[string]bool{},
		pending:      map[pendingKey][]pendingEntry{},
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

// WaitConnected waits until at least one process in pids completes the Hello handshake.
func (h *Host) WaitConnected(pids []uint32, timeout time.Duration) error {
	if len(pids) == 0 {
		return nil
	}
	var chs []<-chan struct{}
	for _, pid := range pids {
		ps := h.getProc(pid)
		if ps == nil {
			continue
		}
		if ps.connected.Load() {
			return nil
		}
		chs = append(chs, ps.helloCh)
	}
	if len(chs) == 0 {
		return errors.New("hook session closed before handshake")
	}
	ready := make(chan struct{}, 1)
	stop := make(chan struct{})
	defer close(stop)
	for _, ch := range chs {
		go func(c <-chan struct{}) {
			select {
			case <-c:
				select {
				case ready <- struct{}{}:
				default:
				}
			case <-stop:
			}
		}(ch)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ready:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for hook handshake after %s", timeout)
	}
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
		NonBlocking:     s.NonBlocking,
		PendingText:     s.PendingText,
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
		h.emitTelem("EmbedCallback", 0, "warn", "popPending miss (late translation after timeout or detach)", map[string]any{
			"pid": tp.ProcessID,
			"key": tp.Key(),
		})
		return
	}
	_ = ps.send(&wireMsg{
		Kind:    msgEmbedResponse,
		ReqID:   reqID,
		HasText: trans != "",
		Text:    trans,
	})
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

	connStart := time.Now()
	downCh := make(chan error, 1)
	go func() { downCh <- ps.down.connect() }()

	upErr := ps.up.connect()
	if upErr != nil {
		_ = windows.SetEvent(ps.stopEvent)
	}
	downErr := <-downCh
	if upErr != nil || downErr != nil {
		ps.host.emitTelem("procSession.serve", time.Since(connStart).Microseconds(), "error", "pipe connect failed", map[string]any{
			"pid": ps.pid,
		})
		ps.cleanupUnexpected()
		return
	}
	ps.host.emitTelem("procSession.serve", time.Since(connStart).Microseconds(), "info", "pipes connected", map[string]any{
		"pid": ps.pid,
	})
	ps.closeHandshake()

	for {
		m, err := readMsg(ps.up)
		if err != nil {
			break
		}
		if !ps.dispatch(m) {
			break
		}
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

func (ps *procSession) dispatch(m *wireMsg) bool {
	hd := ps.host.currentHandler()
	switch m.Kind {
	case msgHello:
		if m.Proto != protoVersion {
			msg := fmt.Sprintf("protocol version mismatch: hook=%d host=%d (restart the game to load the updated hook)", m.Proto, protoVersion)
			ps.host.emitTelem("procSession.dispatch", 0, "error", msg, map[string]any{
				"pid":   ps.pid,
				"proto": m.Proto,
			})
			ps.host.warn(msg)
			return false
		}
		ps.mu.Lock()
		ws := ps.lastSettings
		ps.connected.Store(true)
		ps.mu.Unlock()
		ps.helloOnce.Do(func() { close(ps.helloCh) })
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
	return true
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
	now := time.Now()
	ps.mu.Lock()
	if len(ps.pending) >= 64 {
		cutoff := now.Add(-30 * time.Second)
		for pk, list := range ps.pending {
			kept := list[:0]
			for _, e := range list {
				if e.at.After(cutoff) {
					kept = append(kept, e)
				}
			}
			if len(kept) == 0 {
				delete(ps.pending, pk)
			} else {
				ps.pending[pk] = kept
			}
		}
	}
	ps.pending[k] = append(ps.pending[k], pendingEntry{id: reqID, at: now})
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
	id := q[0].id
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

func (ps *procSession) closeHandshake() {
	ps.shmMu.Lock()
	defer ps.shmMu.Unlock()
	if ps.shmAddr != 0 {
		p := *(*unsafe.Pointer)(unsafe.Pointer(&ps.shmAddr))
		dst := unsafe.Slice((*byte)(p), 4)
		clear(dst)
		_ = windows.UnmapViewOfFile(ps.shmAddr)
		ps.shmAddr = 0
	}
	if ps.shmHandle != 0 {
		_ = windows.CloseHandle(ps.shmHandle)
		ps.shmHandle = 0
	}
}

func (ps *procSession) closeHandles() {
	ps.closeHandshake()
	ps.mu.Lock()
	ps.pending = map[pendingKey][]pendingEntry{}
	ps.mu.Unlock()
	if ps.up != nil {
		ps.up.close()
	}
	ps.writeMu.Lock()
	if ps.down != nil {
		ps.down.close()
	}
	if ps.stopEvent != 0 {
		_ = windows.CloseHandle(ps.stopEvent)
		ps.stopEvent = 0
	}
	ps.writeMu.Unlock()
}

func handshakeMappingName(pid uint32) string {
	return fmt.Sprintf(`Local\khutaf_ipc_%d`, pid)
}

func ownerOnlySecurityDescriptor() (*windows.SECURITY_DESCRIPTOR, error) {
	tok := windows.GetCurrentProcessToken()
	tu, err := tok.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("token user: %w", err)
	}
	sddl := fmt.Sprintf("D:P(A;;GA;;;%s)(A;;GA;;;SY)(A;;GA;;;BA)", tu.User.Sid.String())

	// Grant the current AppContainer SID when running inside a sandboxed process.
	const tokenAppContainerSid = 31
	var buf [16]uintptr
	var retLen uint32
	if err := windows.GetTokenInformation(tok, tokenAppContainerSid, (*byte)(unsafe.Pointer(&buf[0])), uint32(unsafe.Sizeof(buf)), &retLen); err == nil && retLen >= uint32(unsafe.Sizeof(uintptr(0))) {
		if acPtr := *(*unsafe.Pointer)(unsafe.Pointer(&buf[0])); acPtr != nil {
			acSid := (*windows.SID)(acPtr)
			sddl += fmt.Sprintf("(A;;GA;;;%s)", acSid.String())
		}
	}
	return windows.SecurityDescriptorFromString(sddl)
}

func newSecurityAttributes(sd *windows.SECURITY_DESCRIPTOR) *windows.SecurityAttributes {
	if sd == nil {
		return nil
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
	}
}

func createHandshakeMapping(pid uint32, nonce string, sd *windows.SECURITY_DESCRIPTOR) (windows.Handle, uintptr, error) {
	wname, err := windows.UTF16PtrFromString(handshakeMappingName(pid))
	if err != nil {
		return 0, 0, err
	}
	sa := newSecurityAttributes(sd)
	h, err := windows.CreateFileMapping(windows.InvalidHandle, sa, windows.PAGE_READWRITE, 0, handshakeSize, wname)
	runtime.KeepAlive(sa)
	runtime.KeepAlive(sd)
	runtime.KeepAlive(wname)
	if err != nil {
		return 0, 0, fmt.Errorf("CreateFileMapping: %w", err)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(h)
		return 0, 0, errors.New("handshake mapping already exists")
	}
	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_WRITE, 0, 0, handshakeSize)
	if err != nil {
		_ = windows.CloseHandle(h)
		return 0, 0, fmt.Errorf("MapViewOfFile: %w", err)
	}
	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	dst := unsafe.Slice((*byte)(p), handshakeSize)
	clear(dst)
	binary.LittleEndian.PutUint32(dst[4:8], handshakeVersion)
	binary.LittleEndian.PutUint32(dst[8:12], pid)
	binary.LittleEndian.PutUint32(dst[12:16], uint32(len(nonce)))
	copy(dst[16:48], nonce)
	// Write magic last so readers never observe a half-initialized block.
	copy(dst[0:4], handshakeMagic[:])
	return h, addr, nil
}

func readHandshakeNonce(pid uint32) (string, error) {
	wname, err := windows.UTF16PtrFromString(handshakeMappingName(pid))
	if err != nil {
		return "", err
	}
	r, _, openErr := procOpenFileMappingW.Call(windows.FILE_MAP_READ, 0, uintptr(unsafe.Pointer(wname)))
	runtime.KeepAlive(wname)
	if r == 0 {
		return "", fmt.Errorf("OpenFileMappingW(%d): %w", pid, openErr)
	}
	h := windows.Handle(r)
	defer windows.CloseHandle(h)

	addr, err := windows.MapViewOfFile(h, windows.FILE_MAP_READ, 0, 0, handshakeSize)
	if err != nil {
		return "", err
	}
	defer func() { _ = windows.UnmapViewOfFile(addr) }()

	p := *(*unsafe.Pointer)(unsafe.Pointer(&addr))
	var buf [handshakeSize]byte
	copy(buf[:], unsafe.Slice((*byte)(p), handshakeSize))

	if string(buf[0:4]) != string(handshakeMagic[:]) {
		return "", errors.New("invalid handshake magic")
	}
	if binary.LittleEndian.Uint32(buf[4:8]) != handshakeVersion {
		return "", errors.New("invalid handshake version")
	}
	if binary.LittleEndian.Uint32(buf[8:12]) != pid {
		return "", errors.New("handshake PID mismatch")
	}
	if binary.LittleEndian.Uint32(buf[12:16]) != handshakeNonceBytes*2 {
		return "", errors.New("invalid handshake nonce length")
	}
	nonce := string(buf[16:48])
	if _, err := hex.DecodeString(nonce); err != nil {
		return "", fmt.Errorf("invalid handshake nonce: %w", err)
	}
	return nonce, nil
}

// ---- overlapped Windows named pipe wrapper ----

type overlappedPipe struct {
	h         windows.Handle
	ioEvent   windows.Handle
	stopEvent windows.Handle
}

func createOverlappedPipe(name string, openMode uint32, stopEvent windows.Handle, sd *windows.SECURITY_DESCRIPTOR) (*overlappedPipe, error) {
	wname, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	sa := newSecurityAttributes(sd)
	const pipeRejectRemoteClients = 0x00000008
	h, err := windows.CreateNamedPipe(
		wname,
		openMode|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|pipeRejectRemoteClients,
		1,
		pipeBufSize,
		pipeBufSize,
		0,
		sa,
	)
	runtime.KeepAlive(sa)
	runtime.KeepAlive(sd)
	runtime.KeepAlive(wname)
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
