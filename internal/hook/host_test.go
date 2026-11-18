//go:build windows

package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu       sync.Mutex
	host     *Host
	connect  chan uint32
	remove   chan uint32
	newHook  chan HookInfo
	embedReq chan struct {
		text string
		tp   ThreadParam
	}
	outputs []string
}

func newTestHandler(h *Host) *testHandler {
	return &testHandler{
		host:    h,
		connect: make(chan uint32, 4),
		remove:  make(chan uint32, 4),
		newHook: make(chan HookInfo, 4),
		embedReq: make(chan struct {
			text string
			tp   ThreadParam
		}, 4),
	}
}

func (t *testHandler) OnProcessConnect(pid uint32) {
	t.host.SettingsEx(pid, EmbedSettings{WaitTimeMs: 1200, FontFamily: "Arial", FontSizeRelative: 1})
	t.connect <- pid
}
func (t *testHandler) OnProcessRemove(pid uint32) { t.remove <- pid }
func (t *testHandler) OnNewHook(h HookInfo)       { t.newHook <- h }
func (t *testHandler) OnRemoveHook(HookInfo)      {}
func (t *testHandler) OnOutput(_ HookInfo, text string) {
	t.mu.Lock()
	t.outputs = append(t.outputs, text)
	t.mu.Unlock()
}
func (t *testHandler) OnHostInfo(HostInfo, string)               {}
func (t *testHandler) OnHookInsert(uint32, uint64, string)       {}
func (t *testHandler) OnEmbedText(text string, tp ThreadParam) {
	t.embedReq <- struct {
		text string
		tp   ThreadParam
	}{text: text, tp: tp}
}

func TestHostPipeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, nativeHookDLL), []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := NewHost(dir)
	if err != nil {
		t.Fatal(err)
	}
	hd := newTestHandler(h)
	if err := h.Start(hd); err != nil {
		t.Fatal(err)
	}

	pid := uint32(os.Getpid()) + 900000
	h.ConnectProcess(pid)
	defer h.DetachProcess(pid)

	up, err := os.OpenFile(fmt.Sprintf(`\\.\pipe\khutaf_%d_up`, pid), os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open up pipe: %v", err)
	}
	defer up.Close()

	down, err := os.OpenFile(fmt.Sprintf(`\\.\pipe\khutaf_%d_down`, pid), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open down pipe: %v", err)
	}
	defer down.Close()

	if err := writeMsg(up, &wireMsg{
		Kind:         msgHello,
		Proto:        protoVersion,
		PID:          pid,
		Arch:         64,
		Engine:       "unity-mono",
		UnityVersion: "2022.3.10f1",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-hd.connect:
		if got != pid {
			t.Fatalf("connected pid = %d, want %d", got, pid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OnProcessConnect")
	}

	// Host pushes cached/OnProcessConnect Settings on Hello.
	m, err := readMsg(down)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if m.Kind != msgSettings || m.Settings.WaitMs != 1200 || m.Settings.FontFamily != "Arial" {
		t.Fatalf("unexpected settings msg: %+v", m)
	}

	// HookAdded registers hook metadata; first Text from a call site triggers OnNewHook.
	if err := writeMsg(up, &wireMsg{
		Kind:       msgHookAdded,
		HookID:     1,
		Code:       "UNITY:TMP_Text.set_text",
		Name:       "TMPro.TMP_Text::set_text",
		Embeddable: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeMsg(up, &wireMsg{
		Kind:   msgText,
		HookID: 1,
		Ctx:    0x12ab,
		Text:   "Hello!",
	}); err != nil {
		t.Fatal(err)
	}

	var hookInfo HookInfo
	select {
	case hookInfo = <-hd.newHook:
		if hookInfo.HookName != "TMPro.TMP_Text::set_text" || hookInfo.Ctx != 0x12ab {
			t.Fatalf("unexpected hookInfo: %+v", hookInfo)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OnNewHook")
	}

	h.UseEmbed(hookInfo.TP, true)
	m, err = readMsg(down)
	if err != nil {
		t.Fatalf("read set_embed: %v", err)
	}
	if m.Kind != msgSetEmbed || m.HookID != 1 || m.Ctx != 0x12ab || !m.On {
		t.Fatalf("unexpected set_embed msg: %+v", m)
	}

	// EmbedRequest -> EmbedCallback -> EmbedResponse
	if err := writeMsg(up, &wireMsg{
		Kind:   msgEmbedRequest,
		ReqID:  42,
		HookID: 1,
		Ctx:    0x12ab,
		Text:   "Hello!",
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case req := <-hd.embedReq:
		if req.text != "Hello!" || req.tp != hookInfo.TP {
			t.Fatalf("unexpected embed req: %+v", req)
		}
		h.EmbedCallback(req.tp, req.text, "مرحبا!")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OnEmbedText")
	}

	m, err = readMsg(down)
	if err != nil {
		t.Fatalf("read embed_response: %v", err)
	}
	if m.Kind != msgEmbedResponse || m.ReqID != 42 || !m.HasText || m.Text != "مرحبا!" {
		t.Fatalf("unexpected embed response: %+v", m)
	}

	// Detach -> Detach frame sent + OnProcessRemove fired.
	doneDetach := make(chan struct{})
	go func() {
		h.DetachProcess(pid)
		close(doneDetach)
	}()

	m, err = readMsg(down)
	if err != nil {
		t.Fatalf("read detach: %v", err)
	}
	if m.Kind != msgDetach {
		t.Fatalf("expected detach, got 0x%02x", m.Kind)
	}

	select {
	case <-doneDetach:
	case <-time.After(3 * time.Second):
		t.Fatal("DetachProcess hung")
	}

	select {
	case got := <-hd.remove:
		if got != pid {
			t.Fatalf("removed pid = %d, want %d", got, pid)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for OnProcessRemove")
	}
}
