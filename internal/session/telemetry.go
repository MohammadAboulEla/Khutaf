//go:build windows

package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	telemetryRingCap = 2000
	maxStatsKeys     = 256
)

// LogSubsystem identifies the subsystem emitting a telemetry event.
type LogSubsystem string

const (
	SubHookNative    LogSubsystem = "HOOK_NATIVE"
	SubUnityFont     LogSubsystem = "UNITY_FONT"
	SubIPCPipe       LogSubsystem = "IPC_PIPE"
	SubPipelineCache LogSubsystem = "PIPELINE_CACHE"
	SubTranslator    LogSubsystem = "TRANSLATOR"
	SubArabicShaper  LogSubsystem = "ARABIC_SHAPER"
)

// TelemetryEvent represents a structured log or critical function execution record.
type TelemetryEvent struct {
	Seq        uint64         `json:"seq"`
	Timestamp  string         `json:"timestamp"` // "15:04:05.000"
	UnixMs     int64          `json:"unixMs"`
	Level      string         `json:"level"` // "trace" | "debug" | "info" | "warn" | "error"
	Subsystem  LogSubsystem   `json:"subsystem"`
	Func       string         `json:"func"`
	Message    string         `json:"msg"`
	DurationUs int64          `json:"durationUs,omitempty"`
	Fields     map[string]any `json:"fields,omitempty"`
}

// FunctionHealthStats aggregates real-time metrics per watched critical function.
type FunctionHealthStats struct {
	Func         string       `json:"func"`
	Subsystem    LogSubsystem `json:"subsystem"`
	Calls        uint64       `json:"calls"`
	Errors       uint64       `json:"errors"`
	Timeouts     uint64       `json:"timeouts"`
	LastDurUs    int64        `json:"lastDurUs"`
	MaxDurUs     int64        `json:"maxDurUs"`
	AvgDurUs     int64        `json:"avgDurUs"`
	LastError    string       `json:"lastError,omitempty"`
	LastCalledAt string       `json:"lastCalledAt"`
	totalDurUs   int64
}

// TelemetryBatch is emitted periodically to the UI.
type TelemetryBatch struct {
	Events        []TelemetryEvent      `json:"events"`
	Stats         []FunctionHealthStats `json:"stats"`
	DroppedEvents uint64                `json:"droppedEvents"`
}

// TelemetrySnapshot is the full ring-buffer + health matrix state.
type TelemetrySnapshot struct {
	Events        []TelemetryEvent      `json:"events"`
	Stats         []FunctionHealthStats `json:"stats"`
	DroppedEvents uint64                `json:"droppedEvents"`
}

type telemetryStore struct {
	mu      sync.Mutex
	seq     uint64
	ring    [telemetryRingCap]TelemetryEvent
	head    int // next write index
	count   int
	pending []TelemetryEvent
	stats   map[string]*FunctionHealthStats
}

func newTelemetryStore() *telemetryStore {
	return &telemetryStore{
		stats: make(map[string]*FunctionHealthStats),
	}
}

func (t *telemetryStore) record(sub LogSubsystem, fnName, level, msg string, durUs int64, isTimeout bool, errStr string, fields map[string]any) TelemetryEvent {
	now := time.Now()
	level = normalizeTelemetryLevel(level)
	if sub == "" {
		sub = SubHookNative
	}
	if fnName == "" {
		fnName = "unknown"
	}

	t.mu.Lock()
	t.seq++
	ev := TelemetryEvent{
		Seq:        t.seq,
		Timestamp:  now.Format("15:04:05.000"),
		UnixMs:     now.UnixMilli(),
		Level:      level,
		Subsystem:  sub,
		Func:       fnName,
		Message:    msg,
		DurationUs: durUs,
		Fields:     fields,
	}
	t.ring[t.head] = ev
	t.head = (t.head + 1) % telemetryRingCap
	if t.count < telemetryRingCap {
		t.count++
	}
	if len(t.pending) < telemetryRingCap {
		t.pending = append(t.pending, ev)
	}

	statsKey := string(sub) + ":" + fnName
	st, ok := t.stats[statsKey]
	if !ok && len(t.stats) < maxStatsKeys {
		st = &FunctionHealthStats{
			Func:      fnName,
			Subsystem: sub,
		}
		t.stats[statsKey] = st
	}
	if st != nil {
		st.Calls++
		st.LastDurUs = durUs
		st.totalDurUs += durUs
		st.AvgDurUs = st.totalDurUs / int64(st.Calls)
		if durUs > st.MaxDurUs {
			st.MaxDurUs = durUs
		}
		if level == "error" || errStr != "" {
			st.Errors++
			if errStr != "" {
				st.LastError = errStr
			} else {
				st.LastError = msg
			}
		}
		if isTimeout {
			st.Timeouts++
		}
		st.LastCalledAt = ev.Timestamp
	}
	t.mu.Unlock()
	return ev
}

func (t *telemetryStore) flushPending(dropped uint64) (TelemetryBatch, bool) {
	t.mu.Lock()
	if len(t.pending) == 0 {
		t.mu.Unlock()
		return TelemetryBatch{}, false
	}
	events := t.pending
	t.pending = nil
	stats := t.statsLocked()
	t.mu.Unlock()
	return TelemetryBatch{
		Events:        events,
		Stats:         stats,
		DroppedEvents: dropped,
	}, true
}

func (t *telemetryStore) snapshot(dropped uint64) TelemetrySnapshot {
	t.mu.Lock()
	events := make([]TelemetryEvent, 0, t.count)
	if t.count > 0 {
		start := (t.head - t.count + telemetryRingCap) % telemetryRingCap
		for i := 0; i < t.count; i++ {
			events = append(events, t.ring[(start+i)%telemetryRingCap])
		}
	}
	stats := t.statsLocked()
	t.mu.Unlock()
	return TelemetrySnapshot{
		Events:        events,
		Stats:         stats,
		DroppedEvents: dropped,
	}
}

func (t *telemetryStore) clear() {
	t.mu.Lock()
	t.head = 0
	t.count = 0
	t.pending = nil
	t.stats = make(map[string]*FunctionHealthStats)
	t.mu.Unlock()
}

func (t *telemetryStore) statsLocked() []FunctionHealthStats {
	out := make([]FunctionHealthStats, 0, len(t.stats))
	for _, st := range t.stats {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subsystem != out[j].Subsystem {
			return out[i].Subsystem < out[j].Subsystem
		}
		return out[i].Func < out[j].Func
	})
	return out
}

func normalizeTelemetryLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "trace":
		return "trace"
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error", "err":
		return "error"
	default:
		return "info"
	}
}

func normalizeSubsystem(s string) LogSubsystem {
	switch LogSubsystem(strings.TrimSpace(s)) {
	case SubHookNative, SubUnityFont, SubIPCPipe, SubPipelineCache, SubTranslator, SubArabicShaper:
		return LogSubsystem(strings.TrimSpace(s))
	default:
		return SubHookNative
	}
}

// ParseNativeTelemetry parses Rust DLL log lines formatted as:
// [telemetry|<SUBSYSTEM>|<FUNC>|<DURATION_US>|<LEVEL>] <MESSAGE> | key=val ...
func ParseNativeTelemetry(raw, defaultLevel string) (TelemetryEvent, bool) {
	if !strings.HasPrefix(raw, "[telemetry|") {
		return TelemetryEvent{}, false
	}
	closeIdx := strings.IndexByte(raw, ']')
	if closeIdx <= len("[telemetry|") {
		return TelemetryEvent{}, false
	}
	header := raw[len("[telemetry|"):closeIdx]
	parts := strings.Split(header, "|")
	if len(parts) < 3 {
		return TelemetryEvent{}, false
	}

	sub := normalizeSubsystem(parts[0])
	fnName := strings.TrimSpace(parts[1])
	durUs, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	lvl := normalizeTelemetryLevel(defaultLevel)
	if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
		lvl = normalizeTelemetryLevel(parts[3])
	}

	body := strings.TrimSpace(raw[closeIdx+1:])
	msg := body
	var fields map[string]any
	if sep := strings.Index(body, " | "); sep >= 0 {
		msg = strings.TrimSpace(body[:sep])
		kvPart := strings.TrimSpace(body[sep+3:])
		if kvPart != "" {
			fields = make(map[string]any)
			for _, tok := range strings.Fields(kvPart) {
				if eq := strings.IndexByte(tok, '='); eq > 0 {
					k := tok[:eq]
					v := tok[eq+1:]
					fields[k] = v
				}
			}
			if len(fields) == 0 {
				fields = nil
			}
		}
	}

	return TelemetryEvent{
		Level:      lvl,
		Subsystem:  sub,
		Func:       fnName,
		Message:    msg,
		DurationUs: durUs,
		Fields:     fields,
	}, true
}

func isTimeoutMsg(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "timeout") || strings.Contains(low, "timed out") || strings.Contains(low, "deadline")
}

// RecordSpan records a completed function execution span into the telemetry ring buffer.
func (s *Session) RecordSpan(subsystem LogSubsystem, fnName string, start time.Time, err error, fields map[string]any) {
	durUs := time.Since(start).Microseconds()
	lvl := "debug"
	msg := "ok"
	isTimeout := false
	if err != nil {
		lvl = "error"
		msg = err.Error()
		isTimeout = isTimeoutMsg(msg)
	}
	s.recordTelemetry(subsystem, fnName, lvl, msg, durUs, isTimeout, fields)
}

func (s *Session) recordTelemetry(subsystem LogSubsystem, fnName, level, msg string, durUs int64, isTimeout bool, fields map[string]any) {
	lvl := normalizeTelemetryLevel(level)
	if !s.debugMode.Load() && (lvl == "trace" || lvl == "debug") {
		return
	}
	errStr := ""
	if lvl == "error" {
		errStr = msg
	}
	s.telemetry.record(subsystem, fnName, lvl, msg, durUs, isTimeout, errStr, fields)
}

func (s *Session) recordRawTelemetry(sub, fnName string, durUs int64, level, msg string, fields map[string]any) {
	s.recordTelemetry(normalizeSubsystem(sub), fnName, level, msg, durUs, isTimeoutMsg(msg), fields)
}

// GetTelemetrySnapshot returns the current ring-buffer events, function health stats, and dropped count.
func (s *Session) GetTelemetrySnapshot() TelemetrySnapshot {
	return s.telemetry.snapshot(s.droppedEvents.Load())
}

// ClearTelemetry resets the telemetry ring buffer and function health stats.
func (s *Session) ClearTelemetry() {
	s.telemetry.clear()
	s.droppedEvents.Store(0)
	s.post(EventTelemetryBatch, TelemetryBatch{
		Events:        []TelemetryEvent{},
		Stats:         []FunctionHealthStats{},
		DroppedEvents: 0,
	})
}

var (
	reURLUserInfo    = regexp.MustCompile(`(https?://)[^/@\s]+@`)
	reWinUsersPath   = regexp.MustCompile(`(?i)([A-Za-z]:\\Users\\)[^\\/\s"']+`)
	reCLIAPIKeyEqual = regexp.MustCompile(`(?i)(--?(?:api[_-]?key|token|secret)\s*=\s*)(\S+)`)
	reCLIAPIKeySpace = regexp.MustCompile(`(?i)(--?(?:api[_-]?key|token|secret)\s+)("[^"]*"|'[^']*'|\S+)`)
)

func redactString(s string) string {
	if s == "" {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "<USER_HOME>")
	}
	s = reWinUsersPath.ReplaceAllString(s, `${1}<USER>`)
	s = reURLUserInfo.ReplaceAllString(s, `${1}***@`)
	s = reCLIAPIKeyEqual.ReplaceAllString(s, `${1}***`)
	s = reCLIAPIKeySpace.ReplaceAllString(s, `${1}***`)
	return s
}

func sanitizeConfigForExport(c config.Config) config.Config {
	if c.Translator.LLM.APIKey != "" {
		c.Translator.LLM.APIKey = "***"
	}
	c.Translator.LLM.BaseURL = redactString(c.Translator.LLM.BaseURL)
	c.Translator.OfflineLLM.BaseURL = redactString(c.Translator.OfflineLLM.BaseURL)
	c.Translator.LlamaCpp.BinaryPath = redactString(c.Translator.LlamaCpp.BinaryPath)
	c.Translator.LlamaCpp.ModelPath = redactString(c.Translator.LlamaCpp.ModelPath)
	c.Translator.LlamaCpp.ExtraArgs = redactString(c.Translator.LlamaCpp.ExtraArgs)
	return c
}

// ExportDiagnosticBundle writes a .jsonl diagnostic report (status, sanitized config,
// function health stats, telemetry ring buffer, and llama-server logs) and returns its path.
func (s *Session) ExportDiagnosticBundle() (string, error) {
	dir := ""
	if s.store != nil {
		dir = s.store.Dir()
	}
	if dir == "" {
		dir = os.TempDir()
	}
	outPath := filepath.Join(dir, fmt.Sprintf("khutaf-diagnostic-%s.jsonl", time.Now().Format("20060102-150405")))
	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}

	w := bufio.NewWriter(f)
	snap := s.GetTelemetrySnapshot()
	st := s.Status()
	st.GamePath = redactString(st.GamePath)
	meta := map[string]any{
		"type":          "header",
		"exportedAt":    time.Now().Format(time.RFC3339Nano),
		"status":        st,
		"config":        sanitizeConfigForExport(s.Config()),
		"droppedEvents": snap.DroppedEvents,
		"stats":         snap.Stats,
		"llamaLogs":     redactString(s.llama.Status().RecentLogs),
	}
	writeJSONLine(w, meta)
	for _, ev := range snap.Events {
		writeJSONLine(w, map[string]any{
			"type":  "event",
			"event": ev,
		})
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return outPath, nil
}

func writeJSONLine(w *bufio.Writer, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
}
