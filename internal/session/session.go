//go:build windows

// Package session glues the native hook host, per-game profiles, the text
// pipeline, the translator and the Arabic engine together, and emits UI events.
package session

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
	"khutaf/internal/translator"
)

// Emitter delivers an event to the UI.
type Emitter func(name string, data any)

// Event names emitted to the frontend.
const (
	EventLog            = "log"
	EventStatus         = "session:status"
	EventHookNew        = "hook:new"
	EventHookDel        = "hook:remove"
	EventHookText       = "hook:output"
	EventEmbedLine      = "embed:line"
	EventDebugTrace     = "debug:trace"
	EventTelemetryBatch = "telemetry:batch"
	EventBenchmarkItem  = "translator:bench:item"
	EventBenchmarkDone  = "translator:bench:done"
	EventConfigUpdated  = "config:updated"
	EventHotkeyToast    = "hotkey:toast"
)

// HookView is a hook as shown in the UI.
type HookView struct {
	hook.HookInfo
	Embedding     bool         `json:"embedding"`
	Muted         bool         `json:"muted,omitempty"`
	LastText      string       `json:"lastText,omitempty"`
	ObjectPath    string       `json:"objectPath,omitempty"`
	Category      HookCategory `json:"category"`
	DialogueScore int          `json:"dialogueScore"`
	HitCount      int          `json:"hitCount"`
	UpdatesPerSec float64      `json:"updatesPerSec"`
	LastSeenMs    int64        `json:"lastSeenMs"`
	History       []HookSample `json:"history,omitempty"`
}

func currentUnixMs() int64 { return time.Now().UnixMilli() }

type dialogueTurn struct {
	speaker    string
	original   string
	translated string
}

// Status describes the attached game.
type Status struct {
	HostReady        bool     `json:"hostReady"`
	HostError        string   `json:"hostError"`
	Attached         bool     `json:"attached"`
	GamePath         string   `json:"gamePath"`
	GameName         string   `json:"gameName"`
	Engine           string   `json:"engine"`
	Is64             bool     `json:"is64"`
	PIDs             []uint32 `json:"pids"`
	Provider         string   `json:"provider"`
	FallbackProvider string   `json:"fallbackProvider"`
	Paused           bool     `json:"paused"`
	DebugMode        bool     `json:"debugMode"`
	DebugRealTrans   bool     `json:"debugRealTrans"`
}

// DebugTrace records per-stage execution timings (in microseconds) for one embed call.
type DebugTrace struct {
	Seq         uint64 `json:"seq"`
	Time        string `json:"time"`
	Key         string `json:"key"`
	OrigLen     int    `json:"origLen"`
	Cached      bool   `json:"cached"`
	PreRulesUs  int64  `json:"preRulesUs"`
	TranslateUs int64  `json:"translateUs"`
	PostRulesUs int64  `json:"postRulesUs"`
	ShapeUs     int64  `json:"shapeUs"`
	CallbackUs  int64  `json:"callbackUs"`
	TotalUs     int64  `json:"totalUs"`
	Error       string `json:"error,omitempty"`
}

// EmbedLine reports one embedded translation round-trip.
type EmbedLine struct {
	Key        string `json:"key"`
	Original   string `json:"original"`
	Translated string `json:"translated"`
	Shaped     string `json:"shaped"`
	Cached     bool   `json:"cached"`
	Error      string `json:"error,omitempty"`
	ElapsedMs  int64  `json:"elapsedMs"`
}

type embedJob struct {
	text string
	tp   hook.ThreadParam
}

type rules struct {
	pre      *pipeline.RuleSet
	post     *pipeline.RuleSet
	glossary *pipeline.Glossary
}

// Session is the application core. Create with New.
type Session struct {
	store      *config.Store
	translator *translator.Manager
	llama      *translator.LlamaCppManager
	host       *hook.Host
	hostErr    error
	runtimeDir string
	emit       Emitter
	events     chan func()
	embedCh    chan embedJob
	eventStop  chan struct{}
	closeOnce  sync.Once

	rules          atomic.Pointer[rules]
	embedCfg       atomic.Pointer[config.EmbedSettings]
	paused         atomic.Bool
	debugMode      atomic.Bool
	debugRealTrans atomic.Bool
	debugSeq       atomic.Uint64
	telemetry      *telemetryStore
	droppedEvents  atomic.Uint64

	lastEmbedMu sync.Mutex
	lastEmbed   *embedJob

	autoAttachMu   sync.Mutex
	detachCooldown map[string]time.Time
	runningPathsFn func(map[string]bool) map[string]uint32
	autoAttachFn   func(uint32) error

	mu              sync.Mutex
	gamePath        string
	engine          string // engine reported by khutaf-hook64 or detected from files
	is64            bool
	pids            map[uint32]bool
	hooks           map[string]hook.HookInfo
	hookAddrs       map[string]map[uint32]uint64
	embedding       map[string]bool
	muted           map[string]bool
	lastOutput      map[string]string
	hookStats       map[string]*hookIntel
	pendingMeta     map[hookMetaKey]string
	lastDialogueMs  int64
	lastDialogueKey string

	dialogueCtxMu sync.Mutex
	dialogueCtx   []dialogueTurn
	lastSpeaker   string
	lastSpeakerAt time.Time

	benchMu         sync.Mutex
	benchCancel     context.CancelFunc
	providerBuilder func(config.TranslatorSettings) translator.Provider

	unityFontMu    sync.Mutex
	unityFontState UnityFontStatus
}

// New loads the native host from runtimeDir (failure is non-fatal: the UI still
// works for previews and settings) and starts the event dispatcher.
func New(store *config.Store, runtimeDir string, emit Emitter) *Session {
	if store == nil {
		store = config.NewMemoryStore()
	}
	var cache *pipeline.Cache
	var cacheErr error
	if store.Dir() != "" {
		cache, cacheErr = pipeline.OpenCache(filepath.Join(store.Dir(), "cache.db"))
	}
	if cache == nil {
		cache = pipeline.NewMemoryCache()
	}

	s := &Session{
		store:          store,
		translator:     translator.NewManager(cache),
		llama:          translator.NewLlamaCppManager(),
		runtimeDir:     runtimeDir,
		emit:           emit,
		events:         make(chan func(), 4096),
		embedCh:        make(chan embedJob, 256),
		eventStop:      make(chan struct{}),
		telemetry:      newTelemetryStore(),
		pids:           map[uint32]bool{},
		hooks:          map[string]hook.HookInfo{},
		hookAddrs:      map[string]map[uint32]uint64{},
		embedding:      map[string]bool{},
		muted:          map[string]bool{},
		lastOutput:     map[string]string{},
		hookStats:      map[string]*hookIntel{},
		pendingMeta:    map[hookMetaKey]string{},
		detachCooldown: map[string]time.Time{},
		runningPathsFn: hook.RunningPathsFor,
	}
	s.rules.Store(&rules{
		pre:      &pipeline.RuleSet{},
		post:     &pipeline.RuleSet{},
		glossary: &pipeline.Glossary{},
	})
	defEmbed := config.Default().Embed
	s.embedCfg.Store(&defEmbed)
	s.debugRealTrans.Store(true)
	go func() {
		for {
			select {
			case <-s.eventStop:
				return
			case fn := <-s.events:
				fn()
			}
		}
	}()
	for range 8 {
		go func() {
			for {
				select {
				case <-s.eventStop:
					return
				case job := <-s.embedCh:
					s.handleEmbed(job.text, job.tp)
				}
			}
		}()
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.eventStop:
				return
			case <-ticker.C:
				if batch, ok := s.telemetry.flushPending(s.droppedEvents.Load()); ok {
					s.post(EventTelemetryBatch, batch)
				}
			}
		}
	}()
	if cacheErr != nil {
		s.log("warn", "translation cache fallback to memory: "+cacheErr.Error())
	}
	if w := store.InitWarning(); w != "" {
		s.log("warn", w)
	}
	cache.SetTelemetry(s.recordRawTelemetry)
	s.translator.SetTelemetry(s.recordRawTelemetry)
	s.llama.SetTelemetry(s.recordRawTelemetry)
	cfg := store.Config()
	if err := s.applyConfig(cfg); err != nil {
		s.log("warn", err.Error())
	}
	if cfg.Translator.LlamaCpp.AutoStart && cfg.Translator.LlamaCpp.BinaryPath != "" && cfg.Translator.LlamaCpp.ModelPath != "" {
		go func(lcfg config.LlamaCppSettings) {
			if err := s.llama.Start(lcfg); err != nil {
				s.log("warn", "llama-server auto-start failed: "+err.Error())
			} else {
				s.log("info", fmt.Sprintf("llama-server ready on port %d", lcfg.Port))
			}
		}(cfg.Translator.LlamaCpp)
	}
	host, err := hook.NewHost(runtimeDir)
	if err == nil {
		err = host.Start(s)
	}
	if err != nil {
		s.hostErr = err
		s.log("error", "hook host unavailable: "+err.Error())
	} else {
		host.SetTelemetry(s.recordRawTelemetry)
		s.host = host
		s.log("info", "hook host started ("+runtimeDir+")")
	}
	go s.startAutoAttachWatcher()
	return s
}

// Close detaches any active game, stops llama-server, and closes the translation cache database.
func (s *Session) Close() error {
	s.CancelBenchmark()
	s.Detach()
	_ = s.llama.Close()
	err := s.translator.Cache().Close()
	s.closeOnce.Do(func() { close(s.eventStop) })
	return err
}

// StartLlamaServer starts (or restarts) the managed llama-server.exe process.
func (s *Session) StartLlamaServer(cfg config.LlamaCppSettings) (translator.LlamaCppStatus, error) {
	if err := s.llama.Start(cfg); err != nil {
		return s.llama.Status(), err
	}
	st := s.llama.Status()
	s.log("info", fmt.Sprintf("llama-server ready on port %d (pid %d)", st.Port, st.PID))
	return st, nil
}

// StopLlamaServer terminates the managed llama-server.exe process.
func (s *Session) StopLlamaServer() translator.LlamaCppStatus {
	_ = s.llama.Stop()
	return s.llama.Status()
}

// LlamaServerStatus returns the current llama-server status and logs.
func (s *Session) LlamaServerStatus() translator.LlamaCppStatus {
	return s.llama.Status()
}

// post queues UI work so native callbacks never block on the frontend.
func (s *Session) post(name string, data any) {
	if s.emit == nil {
		return
	}
	select {
	case <-s.eventStop:
	case s.events <- func() { s.emit(name, data) }:
	default: // UI is overwhelmed; drop rather than stall the game thread
		s.droppedEvents.Add(1)
	}
}

func (s *Session) log(level, msg string) {
	s.post(EventLog, map[string]string{"level": level, "msg": msg, "time": time.Now().Format("15:04:05")})
}

// ---- status & processes ----

// Status returns a snapshot of the session.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		HostReady:        s.host != nil,
		Attached:         len(s.pids) > 0,
		GamePath:         s.gamePath,
		GameName:         filepath.Base(s.gamePath),
		Engine:           s.engine,
		Is64:             s.is64,
		PIDs:             s.pidList(),
		Provider:         s.translator.Active(),
		FallbackProvider: s.translator.Fallback(),
		Paused:           s.paused.Load(),
		DebugMode:        s.debugMode.Load(),
		DebugRealTrans:   s.debugRealTrans.Load(),
	}
	if s.hostErr != nil {
		st.HostError = s.hostErr.Error()
	}
	if s.gamePath == "" {
		st.GameName = ""
	}
	return st
}

func (s *Session) pidList() []uint32 {
	out := make([]uint32, 0, len(s.pids))
	for pid := range s.pids {
		out = append(out, pid)
	}
	return out
}

// Hooks returns all discovered hooks.
func (s *Session) Hooks() []HookView {
	nowMs := currentUnixMs()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HookView, 0, len(s.hooks))
	for _, h := range s.hooks {
		out = append(out, s.buildHookViewLocked(h, nowMs))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].HookCode != out[j].HookCode {
			return out[i].HookCode < out[j].HookCode
		}
		if out[i].Ctx != out[j].Ctx {
			return out[i].Ctx < out[j].Ctx
		}
		return out[i].Ctx2 < out[j].Ctx2
	})
	return out
}

// SetDebugMode toggles performance profiling mode and optionally keeps real translation active.
func (s *Session) SetDebugMode(enabled bool, realTrans bool) {
	wasMock := s.debugMode.Load() && !s.debugRealTrans.Load()
	nowMock := enabled && !realTrans
	s.debugMode.Store(enabled)
	s.debugRealTrans.Store(realTrans)
	if wasMock != nowMock {
		s.mu.Lock()
		pids := s.pidList()
		s.mu.Unlock()
		if len(pids) > 0 {
			go func() {
				for _, pid := range pids {
					s.pushEmbedSettings(pid)
				}
			}()
		}
	}
	s.post(EventStatus, s.Status())
}

// DebugMode reports whether debug profiling mode is active.
func (s *Session) DebugMode() bool { return s.debugMode.Load() }

// ListProcesses returns attachable windowed processes.
func (s *Session) ListProcesses() ([]hook.ProcessInfo, error) { return hook.ListProcesses() }
