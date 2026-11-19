//go:build windows

package session

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"khutaf/internal/config"
)

func TestTelemetryRingBufferWrapAround(t *testing.T) {
	ts := newTelemetryStore()
	total := telemetryRingCap + 250
	for i := 0; i < total; i++ {
		ts.record(SubTranslator, "Manager.Translate", "debug", "ok", int64(i+1), false, "", nil)
	}
	snap := ts.snapshot(0)
	if len(snap.Events) != telemetryRingCap {
		t.Fatalf("expected %d events, got %d", telemetryRingCap, len(snap.Events))
	}
	if snap.Events[0].DurationUs != 251 {
		t.Fatalf("expected oldest retained duration 251, got %d", snap.Events[0].DurationUs)
	}
	last := snap.Events[len(snap.Events)-1]
	if last.DurationUs != int64(total) {
		t.Fatalf("expected newest duration %d, got %d", total, last.DurationUs)
	}
}

func TestParseNativeTelemetry(t *testing.T) {
	line := `[telemetry|IPC_PIPE|embed::process|850000|warn] embed wait > 80% | hook_id=5 wait_ms=1000`
	ev, ok := ParseNativeTelemetry(line, "info")
	if !ok {
		t.Fatal("expected ParseNativeTelemetry to succeed")
	}
	if ev.Subsystem != SubIPCPipe {
		t.Fatalf("unexpected subsystem: %q", ev.Subsystem)
	}
	if ev.Func != "embed::process" {
		t.Fatalf("unexpected func: %q", ev.Func)
	}
	if ev.DurationUs != 850000 {
		t.Fatalf("unexpected durationUs: %d", ev.DurationUs)
	}
	if ev.Level != "warn" {
		t.Fatalf("unexpected level: %q", ev.Level)
	}
	if ev.Message != "embed wait > 80%" {
		t.Fatalf("unexpected message: %q", ev.Message)
	}
	if ev.Fields["hook_id"] != "5" || ev.Fields["wait_ms"] != "1000" {
		t.Fatalf("unexpected fields: %+v", ev.Fields)
	}
}

func TestFunctionHealthStatsAggregation(t *testing.T) {
	ts := newTelemetryStore()
	ts.record(SubTranslator, "Bing.getSession", "debug", "ok", 100, false, "", nil)
	ts.record(SubTranslator, "Bing.getSession", "warn", "slow", 300, false, "", nil)
	ts.record(SubTranslator, "Bing.getSession", "error", "timeout", 500, true, "timeout", nil)

	snap := ts.snapshot(3)
	if snap.DroppedEvents != 3 {
		t.Fatalf("expected dropped=3, got %d", snap.DroppedEvents)
	}
	if len(snap.Stats) != 1 {
		t.Fatalf("expected 1 stat entry, got %d", len(snap.Stats))
	}
	st := snap.Stats[0]
	if st.Calls != 3 || st.Errors != 1 || st.Timeouts != 1 {
		t.Fatalf("unexpected counts: %+v", st)
	}
	if st.LastDurUs != 500 || st.MaxDurUs != 500 || st.AvgDurUs != 300 {
		t.Fatalf("unexpected durations: last=%d max=%d avg=%d", st.LastDurUs, st.MaxDurUs, st.AvgDurUs)
	}
	if st.LastError != "timeout" {
		t.Fatalf("expected LastError=timeout, got %q", st.LastError)
	}
}

func TestExportDiagnosticBundleMasksAPIKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := store.Config()
	cfg.Translator.LLM.APIKey = "sk-secret-llm-key"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	s := New(store, dir, nil)
	defer s.Close()
	s.recordTelemetry(SubHookNative, "il2cpp::detour", "error", "panic caught", 0, false, map[string]any{"hook_id": 1})

	outPath, err := s.ExportDiagnosticBundle()
	if err != nil {
		t.Fatalf("ExportDiagnosticBundle: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "sk-secret-llm-key") {
		t.Fatalf("exported bundle leaked API key")
	}
	if !strings.Contains(content, `"***"`) {
		t.Fatalf("expected masked API keys in bundle")
	}
	if !strings.Contains(content, `"il2cpp::detour"`) {
		t.Fatalf("expected telemetry event in bundle")
	}
}

func TestDebugModeGatesTraceEvents(t *testing.T) {
	s := New(config.NewMemoryStore(), "", nil)
	defer s.Close()

	s.SetDebugMode(false, true)
	s.recordTelemetry(SubPipelineCache, "Cache.Get", "trace", "miss", 10, false, nil)
	s.recordTelemetry(SubTranslator, "Manager.Translate", "debug", "ok", 20, false, nil)
	s.recordTelemetry(SubIPCPipe, "procSession.serve", "info", "pipes connected", 30, false, nil)
	s.recordTelemetry(SubTranslator, "Bing.postTranslate", "warn", "retry", 40, false, nil)
	s.recordTelemetry(SubHookNative, "il2cpp::detour", "error", "panic", 0, false, nil)

	snap := s.GetTelemetrySnapshot()
	if len(snap.Events) != 3 {
		t.Fatalf("expected 3 non-trace/debug events when debugMode=false, got %d", len(snap.Events))
	}

	s.SetDebugMode(true, true)
	s.recordTelemetry(SubPipelineCache, "Cache.Get", "trace", "hit:auto_game", 5, false, nil)
	s.recordTelemetry(SubTranslator, "Manager.Translate", "debug", "ok", 15, false, nil)

	snap = s.GetTelemetrySnapshot()
	if len(snap.Events) != 5 {
		t.Fatalf("expected 5 total events when debugMode=true, got %d", len(snap.Events))
	}
}

func TestRecordSpanCountsTimedOutErrors(t *testing.T) {
	s := New(config.NewMemoryStore(), "", nil)
	defer s.Close()

	s.RecordSpan(SubIPCPipe, "hook.Inject", time.Now().Add(-5*time.Second), errors.New("LoadLibraryW(18232) timed out (wait status 258)"), nil)
	snap := s.GetTelemetrySnapshot()
	if len(snap.Stats) != 1 {
		t.Fatalf("expected 1 stat entry, got %d", len(snap.Stats))
	}
	if snap.Stats[0].Timeouts != 1 || snap.Stats[0].Errors != 1 {
		t.Fatalf("expected Timeouts=1 Errors=1, got %+v", snap.Stats[0])
	}
}

func TestExportDiagnosticBundleRedactsPathsURLsAndArgs(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cfg := store.Config()
	cfg.Translator.LLM.BaseURL = "https://user:superpass@api.example.com/v1"
	cfg.Translator.LlamaCpp.BinaryPath = `C:\Users\AliceSecret\bin\llama-server.exe`
	cfg.Translator.LlamaCpp.ModelPath = `C:\Users\AliceSecret\models\gemma.gguf`
	cfg.Translator.LlamaCpp.ExtraArgs = `--api-key my-llama-secret --threads 4`
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	s := New(store, dir, nil)
	defer s.Close()
	s.mu.Lock()
	s.gamePath = `C:\Users\AliceSecret\Games\Wonderia.exe`
	s.mu.Unlock()

	outPath, err := s.ExportDiagnosticBundle()
	if err != nil {
		t.Fatalf("ExportDiagnosticBundle: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	content := string(raw)
	for _, secret := range []string{"superpass", "AliceSecret", "my-llama-secret"} {
		if strings.Contains(content, secret) {
			t.Fatalf("exported bundle leaked sensitive string %q", secret)
		}
	}
}

func TestTelemetrySubsystemWhitelistStatsCapAndCompositeKey(t *testing.T) {
	ev, ok := ParseNativeTelemetry(`[telemetry|UNKNOWN_SUB|foo|42|warn] test`, "info")
	if !ok || ev.Subsystem != SubHookNative {
		t.Fatalf("expected unknown subsystem to normalize to HOOK_NATIVE, got %+v (ok=%v)", ev, ok)
	}

	ts := newTelemetryStore()
	ts.record(SubIPCPipe, "sharedName", "info", "a", 10, false, "", nil)
	ts.record(SubTranslator, "sharedName", "info", "b", 20, false, "", nil)
	snap := ts.snapshot(0)
	if len(snap.Stats) != 2 {
		t.Fatalf("expected 2 stats entries for same func across subsystems, got %d", len(snap.Stats))
	}

	for i := 0; i < maxStatsKeys+50; i++ {
		ts.record(SubTranslator, "fn_"+string(rune('A'+i%26))+"_"+time.Now().Format("150405")+string(rune(i)), "info", "x", 1, false, "", nil)
	}
	if len(ts.snapshot(0).Stats) > maxStatsKeys {
		t.Fatalf("stats map exceeded cap %d: got %d", maxStatsKeys, len(ts.snapshot(0).Stats))
	}
}
