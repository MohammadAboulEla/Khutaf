//go:build windows

// Package session glues the native hook host, per-game profiles, the text
// pipeline, the translator and the Arabic engine together, and emits UI events.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"khutaf/internal/arabic"
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
	Embedding bool   `json:"embedding"`
	LastText  string `json:"lastText,omitempty"`
}

// Status describes the attached game.
type Status struct {
	HostReady bool     `json:"hostReady"`
	HostError string   `json:"hostError"`
	Attached  bool     `json:"attached"`
	GamePath  string   `json:"gamePath"`
	GameName  string   `json:"gameName"`
	Engine    string   `json:"engine"`
	Is64      bool     `json:"is64"`
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

	mu         sync.Mutex
	gamePath   string
	engine     string // engine reported by khutaf-hook64 or detected from files
	is64       bool
	pids       map[uint32]bool
	hooks      map[string]hook.HookInfo
	hookAddrs  map[string]map[uint32]uint64
	embedding  map[string]bool
	lastOutput map[string]string

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
		store:      store,
		translator: translator.NewManager(cache),
		llama:      translator.NewLlamaCppManager(),
		runtimeDir: runtimeDir,
		emit:       emit,
		events:     make(chan func(), 4096),
		embedCh:    make(chan embedJob, 256),
		eventStop:  make(chan struct{}),
		telemetry:  newTelemetryStore(),
		pids:           map[uint32]bool{},
		hooks:          map[string]hook.HookInfo{},
		hookAddrs:      map[string]map[uint32]uint64{},
		embedding:      map[string]bool{},
		lastOutput:     map[string]string{},
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

// ---- configuration ----

// Config returns the current settings.
func (s *Session) Config() config.Config { return s.store.Config() }

// SaveConfig validates, persists and applies settings, then pushes embed
// settings to every connected process.
func (s *Session) SaveConfig(c config.Config) error {
	c = normalizeConfig(c)
	prev := s.store.Config()
	if err := s.applyConfig(c); err != nil {
		return err
	}
	if err := s.store.SaveConfig(c); err != nil {
		_ = s.applyConfig(prev)
		return err
	}
	s.mu.Lock()
	pids := s.pidList()
	s.mu.Unlock()
	for _, pid := range pids {
		s.pushEmbedSettings(pid)
	}
	s.post(EventConfigUpdated, c)
	s.post(EventStatus, s.Status())
	return nil
}

func normalizeEmbed(e config.EmbedSettings) config.EmbedSettings {
	if e.TimeoutMs < 100 {
		e.TimeoutMs = 100
	} else if e.TimeoutMs > 30000 {
		e.TimeoutMs = 30000
	}
	if e.MaxLineLength <= 0 {
		e.MaxLineLength = arabic.DefaultLineLength
	}
	if e.FontSizeRelative <= 0 {
		e.FontSizeRelative = 1.0
	}
	return e
}

func normalizeConfig(c config.Config) config.Config {
	c.Embed = normalizeEmbed(c.Embed)
	if c.UI.Language != "ar" && c.UI.Language != "en" {
		c.UI.Language = "ar"
	}
	if c.UI.Mode != "simple" && c.UI.Mode != "advanced" {
		c.UI.Mode = "simple"
	}
	return c
}

func (s *Session) effectiveEmbed(global config.EmbedSettings, gamePath string) config.EmbedSettings {
	if gamePath != "" {
		if eo := s.store.Game(gamePath).EmbedOverride; eo != nil {
			return normalizeEmbed(*eo)
		}
	}
	return normalizeEmbed(global)
}

func (s *Session) effectiveTranslator(global config.TranslatorSettings, gamePath string) config.TranslatorSettings {
	if gamePath != "" {
		if to := s.store.Game(gamePath).TranslatorOverride; to != nil {
			return *to
		}
	}
	return global
}

func (s *Session) applyConfig(c config.Config) error {
	s.mu.Lock()
	gamePath := s.gamePath
	s.mu.Unlock()
	if err := s.rebuildRules(c.Pipeline, gamePath); err != nil {
		return err
	}
	embed := s.effectiveEmbed(c.Embed, gamePath)
	s.embedCfg.Store(&embed)
	s.translator.Configure(s.effectiveTranslator(c.Translator, gamePath))
	return nil
}

func (s *Session) rebuildRules(global config.PipelineSettings, gamePath string) error {
	preList := make([]config.Rule, 0, len(global.PreRules))
	preList = append(preList, global.PreRules...)
	postList := make([]config.Rule, 0, len(global.PostRules))
	postList = append(postList, global.PostRules...)
	var gp config.PipelineSettings
	if gamePath != "" {
		gp = s.store.Game(gamePath).Pipeline
		preList = append(preList, gp.PreRules...)
		postList = append(postList, gp.PostRules...)
	}
	pre, err := pipeline.Compile(preList)
	if err != nil {
		return fmt.Errorf("pre-processing: %w", err)
	}
	post, err := pipeline.Compile(postList)
	if err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	glossary := pipeline.CompileGlossary(global.Glossary, gp.Glossary)
	s.rules.Store(&rules{pre: pre, post: post, glossary: glossary})
	return nil
}

// ---- status & processes ----

// Status returns a snapshot of the session.
func (s *Session) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{
		HostReady: s.host != nil,
		Attached:  len(s.pids) > 0,
		GamePath:  s.gamePath,
		GameName:  filepath.Base(s.gamePath),
		Engine:    s.engine,
		Is64:      s.is64,
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
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HookView, 0, len(s.hooks))
	for k, h := range s.hooks {
		out = append(out, HookView{HookInfo: h, Embedding: s.embedding[k], LastText: s.lastOutput[k]})
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

// SavedGame describes a persisted game profile and whether it is currently running.
type SavedGame struct {
	ExePath            string                     `json:"exePath"`
	Title              string                     `json:"title"`
	EmbedHooks         []config.EmbedHook         `json:"embedHooks"`
	Pipeline           config.PipelineSettings    `json:"pipeline"`
	EmbedOverride      *config.EmbedSettings      `json:"embedOverride,omitempty"`
	TranslatorOverride *config.TranslatorSettings `json:"translatorOverride,omitempty"`
	AutoAttach         bool                       `json:"autoAttach"`
	RunningPID         uint32                     `json:"runningPid"`
	Attached           bool                       `json:"attached"`
}

// SavedGames returns all saved game profiles with live running/attached status.
func (s *Session) SavedGames() []SavedGame {
	games := s.store.Games()
	s.mu.Lock()
	rawGamePath := s.gamePath
	isAttached := len(s.pids) > 0
	s.mu.Unlock()
	curPath := ""
	if rawGamePath != "" {
		curPath = strings.ToLower(filepath.Clean(rawGamePath))
	}

	basenames := make(map[string]bool, len(games))
	for exePath := range games {
		basenames[strings.ToLower(filepath.Base(filepath.Clean(exePath)))] = true
	}
	runningByPath := hook.RunningPathsFor(basenames)

	out := make([]SavedGame, 0, len(games))
	for exePath, prof := range games {
		normExe := strings.ToLower(filepath.Clean(exePath))
		runningPID := runningByPath[normExe]
		title := prof.Title
		if title == "" {
			title = filepath.Base(exePath)
		}
		out = append(out, SavedGame{
			ExePath:            exePath,
			Title:              title,
			EmbedHooks:         prof.EmbedHooks,
			Pipeline:           prof.Pipeline,
			EmbedOverride:      prof.EmbedOverride,
			TranslatorOverride: prof.TranslatorOverride,
			AutoAttach:         prof.AutoAttach,
			RunningPID:         runningPID,
			Attached:           isAttached && curPath != "" && curPath == normExe,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Attached != out[j].Attached {
			return out[i].Attached
		}
		if (out[i].RunningPID != 0) != (out[j].RunningPID != 0) {
			return out[i].RunningPID != 0
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out
}

// QuickAttach attaches to the first running instance of exePath.
func (s *Session) QuickAttach(exePath string) error {
	pids := hook.PIDsByPath(exePath)
	if len(pids) == 0 {
		return fmt.Errorf("%s is not currently running", filepath.Base(exePath))
	}
	return s.Attach(pids[0])
}

// DeleteSavedGame removes a saved game profile from games.json.
func (s *Session) DeleteSavedGame(exePath string) error {
	return s.store.DeleteGame(exePath)
}

// ClearSavedGameHooks removes all saved embed hooks for exePath.
func (s *Session) ClearSavedGameHooks(exePath string) error {
	return s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		g.EmbedHooks = nil
	})
}

// RemoveSavedGameHook removes a single saved embed hook from exePath and disables it live when attached.
func (s *Session) RemoveSavedGameHook(exePath, hookCode string, ctx, ctx2 uint64) error {
	err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		kept := g.EmbedHooks[:0]
		for _, h := range g.EmbedHooks {
			if h.HookCode == hookCode && h.Ctx == ctx && h.Ctx2 == ctx2 {
				continue
			}
			kept = append(kept, h)
		}
		g.EmbedHooks = kept
	})
	if err != nil {
		return err
	}

	s.mu.Lock()
	attachedMatch := s.gamePath != "" && strings.EqualFold(filepath.Clean(s.gamePath), filepath.Clean(exePath))
	var tps []hook.ThreadParam
	var liveDisabled []hook.HookInfo
	if attachedMatch {
		seenTP := map[string]bool{}
		for _, h := range s.hooks {
			if h.HookCode == hookCode && h.Ctx == ctx && h.Ctx2 == ctx2 {
				delete(s.embedding, h.Key)
				liveDisabled = append(liveDisabled, h)
				if !seenTP[h.Key] {
					seenTP[h.Key] = true
					tps = append(tps, h.TP)
				}
			}
		}
		for pid, addr := range s.hookAddrs[hookCode] {
			tp := hook.ThreadParam{ProcessID: pid, Addr: addr, Ctx: ctx, Ctx2: ctx2}
			k := tp.Key()
			delete(s.embedding, k)
			if !seenTP[k] {
				seenTP[k] = true
				tps = append(tps, tp)
			}
		}
	}
	s.mu.Unlock()

	if s.host != nil {
		for _, tp := range tps {
			s.host.UseEmbed(tp, false)
		}
	}
	for _, h := range liveDisabled {
		s.post(EventHookNew, HookView{HookInfo: h, Embedding: false})
	}
	return nil
}

// SaveGamePipeline validates, persists, and (if currently attached) applies per-game rules.
func (s *Session) SaveGamePipeline(exePath string, p config.PipelineSettings) error {
	if _, err := pipeline.Compile(p.PreRules); err != nil {
		return fmt.Errorf("pre-processing: %w", err)
	}
	if _, err := pipeline.Compile(p.PostRules); err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	if err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
		}
		g.Pipeline = p
	}); err != nil {
		return err
	}
	s.mu.Lock()
	curPath := s.gamePath
	s.mu.Unlock()
	if curPath != "" && strings.EqualFold(filepath.Clean(curPath), filepath.Clean(exePath)) {
		return s.rebuildRules(s.store.Config().Pipeline, curPath)
	}
	return nil
}

// SaveGameOverrides saves optional per-game embed/translator overrides (pass nil to inherit global)
// and applies them immediately when exePath is currently attached.
func (s *Session) SaveGameOverrides(exePath string, embedOverride *config.EmbedSettings, translatorOverride *config.TranslatorSettings) error {
	var normEmbed *config.EmbedSettings
	if embedOverride != nil {
		e := normalizeEmbed(*embedOverride)
		normEmbed = &e
	}
	if err := s.store.UpdateGame(exePath, func(g *config.GameProfile) {
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))
		}
		g.EmbedOverride = normEmbed
		g.TranslatorOverride = translatorOverride
	}); err != nil {
		return err
	}
	s.mu.Lock()
	curPath := s.gamePath
	pids := s.pidList()
	s.mu.Unlock()
	if curPath != "" && strings.EqualFold(filepath.Clean(curPath), filepath.Clean(exePath)) {
		if err := s.applyConfig(s.store.Config()); err != nil {
			return err
		}
		for _, pid := range pids {
			s.pushEmbedSettings(pid)
		}
		s.post(EventStatus, s.Status())
	}
	return nil
}

// ClearTranslationCache clears non-manual cached translations and returns the count cleared.
func (s *Session) ClearTranslationCache() int {
	n := s.translator.ClearCache()
	s.log("info", fmt.Sprintf("cleared %d cached translations", n))
	return n
}

// CacheCount returns the total number of cached translations.
func (s *Session) CacheCount() int { return s.translator.CacheSize() }

// ListCacheEntries returns filtered cache entries for the cache browser UI.
func (s *Session) ListCacheEntries(gamePath, query string, manualOnly bool, limit int) []pipeline.CacheEntry {
	return s.translator.Cache().List(gamePath, query, manualOnly, limit)
}

// SaveCacheEntry inserts or updates a persistent manual translation override.
func (s *Session) SaveCacheEntry(gameUID, provider, sourceText, translated string) error {
	if strings.TrimSpace(sourceText) == "" {
		return errors.New("source text is required")
	}
	return s.translator.Cache().PutManual(gameUID, provider, sourceText, translated)
}

// DeleteCacheEntry deletes a single entry from the SQLite translation cache.
func (s *Session) DeleteCacheEntry(gameUID, provider, sourceText string) error {
	return s.translator.Cache().Delete(gameUID, provider, sourceText)
}

// ClearScopeCache clears cached entries for gamePath (or all games when empty).
func (s *Session) ClearScopeCache(gamePath string, includeManual bool) int {
	n := s.translator.Cache().Clear(gamePath, includeManual)
	s.log("info", fmt.Sprintf("cleared %d cached translations", n))
	return n
}

// ---- attach / detach ----

// Attach connects to every running instance of pid's executable and injects khutaf-hook64.dll.
func (s *Session) Attach(pid uint32) error {
	if s.host == nil {
		return fmt.Errorf("hook host unavailable: %v", s.hostErr)
	}
	path, err := hook.ProcessPath(pid)
	if err != nil {
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	is64, err := hook.Is64Bit(pid)
	if err != nil {
		return fmt.Errorf("query process %d: %w", pid, err)
	}
	if !is64 {
		return errors.New("32-bit games are not supported yet (x64 Unity only)")
	}
	engine := hook.DetectEngine(path)
	if !strings.HasPrefix(engine, "unity") {
		return fmt.Errorf("unsupported engine for %s: only 64-bit Unity games are supported yet", filepath.Base(path))
	}
	pids := hook.PIDsByPath(path)
	if len(pids) == 0 {
		pids = []uint32{pid}
	}

	s.Detach()
	s.mu.Lock()
	s.gamePath = path
	s.is64 = is64
	s.engine = engine
	s.mu.Unlock()
	_ = s.applyConfig(s.store.Config())
	s.translator.SetGame(path)

	var needInject []uint32
	for _, p := range pids {
		s.host.ConnectProcess(p)
		if s.host.CheckIfNeedInject(p) {
			needInject = append(needInject, p)
		}
	}
	s.log("info", fmt.Sprintf("attaching to %s (pids %v, %s)", filepath.Base(path), pids, archName(is64)))
	if ac := hook.DetectAntiCheat(pids, path); ac != "" {
		s.log("warn", fmt.Sprintf("detected %s in %s — injecting into anti-cheat or online games may cause bans or crashes", ac, filepath.Base(path)))
	}
	injectStart := time.Now()
	err = hook.Inject(s.runtimeDir, needInject, is64)
	s.RecordSpan(SubIPCPipe, "hook.Inject", injectStart, err, map[string]any{"pids": needInject})
	if err == nil {
		err = s.host.WaitConnected(pids, 8*time.Second)
	}
	if err != nil {
		for _, p := range pids {
			s.host.DetachProcess(p)
		}
		s.Detach()
		s.log("error", err.Error())
		return err
	}
	s.post(EventStatus, s.Status())
	s.clearDetachCooldown(path)
	return nil
}

// Detach unhooks all connected processes and clears hook state.
func (s *Session) Detach() {
	s.mu.Lock()
	prevPath := s.gamePath
	pids := s.pidList()
	s.pids = map[uint32]bool{}
	s.hooks = map[string]hook.HookInfo{}
	s.hookAddrs = map[string]map[uint32]uint64{}
	s.embedding = map[string]bool{}
	s.lastOutput = map[string]string{}
	s.gamePath = ""
	s.engine = ""
	s.mu.Unlock()
	s.setLastEmbed("", hook.ThreadParam{})
	if prevPath != "" {
		s.recordDetachCooldown(prevPath)
	}
	_ = s.applyConfig(s.store.Config())
	s.translator.SetGame("")
	if s.host != nil {
		for _, pid := range pids {
			s.host.DetachProcess(pid)
		}
	}
	s.post(EventStatus, s.Status())
}

// SetEmbed toggles embedded translation for a hook and persists the choice.
func (s *Session) SetEmbed(key string, enabled bool) error {
	if s.host == nil {
		return errors.New("hook host unavailable")
	}
	s.mu.Lock()
	h, ok := s.hooks[key]
	path := s.gamePath
	sample := ""
	if ok {
		if enabled {
			s.embedding[key] = true
			sample = truncateSample(s.lastOutput[key], 120)
		} else {
			delete(s.embedding, key)
		}
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown hook %s", key)
	}
	s.host.UseEmbed(h.TP, enabled)
	if err := s.store.UpdateGame(path, func(g *config.GameProfile) {
		kept := g.EmbedHooks[:0]
		for _, e := range g.EmbedHooks {
			if !(e.HookCode == h.HookCode && e.Ctx == h.Ctx && e.Ctx2 == h.Ctx2) {
				kept = append(kept, e)
			}
		}
		g.EmbedHooks = kept
		if enabled {
			g.EmbedHooks = append(g.EmbedHooks, config.EmbedHook{
				HookCode: h.HookCode,
				HookName: h.HookName,
				Ctx:      h.Ctx,
				Ctx2:     h.Ctx2,
				Sample:   sample,
			})
		}
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}); err != nil {
		return err
	}
	s.post(EventHookNew, HookView{HookInfo: h, Embedding: enabled})
	return nil
}

func truncateSample(raw string, maxRunes int) string {
	clean := strings.TrimSpace(strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(raw))
	if clean == "" || maxRunes <= 0 {
		return ""
	}
	r := []rune(clean)
	if len(r) <= maxRunes {
		return clean
	}
	return strings.TrimSpace(string(r[:maxRunes-1])) + "…"
}

// SmartAutoEmbedResult reports the outcome of one-click auto-enabling Unity text hooks.
type SmartAutoEmbedResult struct {
	EnabledNow      int `json:"enabledNow"`
	TotalActive     int `json:"totalActive"`
	TotalEmbeddable int `json:"totalEmbeddable"`
}

// SmartAutoEmbed automatically enables embedding on the most likely Unity dialogue/UI hooks.
func (s *Session) SmartAutoEmbed() (SmartAutoEmbedResult, error) {
	if s.host == nil {
		return SmartAutoEmbedResult{}, errors.New("hook host unavailable")
	}
	s.mu.Lock()
	if len(s.pids) == 0 {
		s.mu.Unlock()
		return SmartAutoEmbedResult{}, errors.New("no game attached")
	}
	hooksList := make([]hook.HookInfo, 0, len(s.hooks))
	totalEmbeddable := 0
	for _, h := range s.hooks {
		hooksList = append(hooksList, h)
		if h.Embeddable {
			totalEmbeddable++
		}
	}
	lastOut := make(map[string]string, len(s.lastOutput))
	for k, v := range s.lastOutput {
		lastOut[k] = v
	}
	already := make(map[string]bool, len(s.embedding))
	for k, v := range s.embedding {
		if v {
			already[k] = true
		}
	}
	s.mu.Unlock()

	selected := selectSmartEmbedHooks(hooksList, lastOut)
	enabledNow := 0
	for _, key := range selected {
		if already[key] {
			continue
		}
		if err := s.SetEmbed(key, true); err != nil {
			return SmartAutoEmbedResult{}, err
		}
		already[key] = true
		enabledNow++
	}
	return SmartAutoEmbedResult{
		EnabledNow:      enabledNow,
		TotalActive:     len(already),
		TotalEmbeddable: totalEmbeddable,
	}, nil
}

func selectSmartEmbedHooks(hooksList []hook.HookInfo, lastOutput map[string]string) []string {
	var tier1, tier2, tier3 []string
	for _, h := range hooksList {
		if !h.Embeddable {
			continue
		}
		tier3 = append(tier3, h.Key)
		if isUnityTextHook(h) {
			tier2 = append(tier2, h.Key)
			if hasMeaningfulText(lastOutput[h.Key]) {
				tier1 = append(tier1, h.Key)
			}
		}
	}
	if len(tier1) > 0 {
		sort.Strings(tier1)
		return tier1
	}
	if len(tier2) > 0 {
		sort.Strings(tier2)
		return tier2
	}
	sort.Strings(tier3)
	return tier3
}

func isUnityTextHook(h hook.HookInfo) bool {
	src := strings.ToLower(h.HookName + " " + h.HookCode)
	for _, kw := range []string{
		"textmeshprougui", "textmeshpro", "tmp_text",
		"unityengine.ui", "ui.text", "uielements",
		"uilabel", "naninovel", "unity",
	} {
		if strings.Contains(src, kw) {
			return true
		}
	}
	return false
}

func hasMeaningfulText(s string) bool {
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if letters >= 2 {
				return true
			}
		}
	}
	return false
}

func (s *Session) pushEmbedSettings(pid uint32) {
	if s.host == nil {
		return
	}
	cfg := *s.embedCfg.Load()
	s.mu.Lock()
	engine := s.engine
	s.mu.Unlock()
	font := ""
	unityDir := ""
	if cfg.ChangeFont {
		font = cfg.FontFamily
		if cfg.UseUnityBundle && strings.HasPrefix(strings.ToLower(engine), "unity") {
			unityDir = s.unityFontDir()
		}
	}
	waitMs := cfg.TimeoutMs
	if waitMs < 100 {
		waitMs = 100
	} else if waitMs > 30000 {
		waitMs = 30000
	}
	pending := strings.TrimSpace(cfg.PendingText)
	if pending != "" && arabic.HasArabic(pending) {
		pending = s.ShapeForEmbed(pending)
	}
	s.host.SettingsEx(pid, hook.EmbedSettings{
		WaitTimeMs:          uint32(waitMs),
		FontFamily:          font,
		DisplayMode:         hook.DisplayMode(cfg.DisplayMode),
		ClearText:           cfg.ClearText,
		ChangeFontSize:      cfg.ChangeFontSize,
		FontSizeRelative:    cfg.FontSizeRelative,
		UnityFontDir:        unityDir,
		StripTypewriterTags: cfg.StripTypewriterTags,
		NonBlocking:         cfg.NonBlocking,
		PendingText:         pending,
		VerboseHooks:        cfg.VerboseHooks,
	})
}

func (s *Session) unityFontDir() string {
	dir, count := s.unityFontMatches()
	if count > 0 {
		return dir
	}
	return ""
}

// ---- translation ----

// Translate runs the full pipeline (pre-rules, glossary, translator, post-rules,
// placeholder check) and returns the logical (unshaped) translation.
func (s *Session) Translate(ctx context.Context, text string) (string, bool, error) {
	return s.translateWith(ctx, text, s.translator.Active(), nil)
}

// TranslateWithConfig runs the full pipeline using cfg without mutating saved settings or cache.
func (s *Session) TranslateWithConfig(ctx context.Context, cfg config.TranslatorSettings, text string) (string, error) {
	p := translator.BuildProvider(cfg)
	if p == nil {
		return "", errors.New("no translator provider selected")
	}
	trans, _, err := s.translateWith(ctx, text, p.Name(), func(c context.Context, in string) (string, bool, error) {
		out, err := p.Translate(c, in)
		return out, false, err
	})
	return trans, err
}

func isLLMProvider(name string) bool {
	return name == "llm" || name == "offline_llm" ||
		strings.HasPrefix(name, "llm:") || strings.HasPrefix(name, "offline_llm:")
}

func (s *Session) translateWith(ctx context.Context, text, providerName string, fn func(context.Context, string) (string, bool, error)) (string, bool, error) {
	r := s.rules.Load()
	preText := r.pre.Apply(text)
	if isLLMProvider(providerName) {
		if gp := r.glossary.PromptContext(preText); gp != "" {
			ctx = translator.WithGlossaryPrompt(ctx, gp)
		}
	} else {
		preText = r.glossary.Apply(preText)
	}
	var (
		trans  string
		cached bool
		err    error
	)
	if fn != nil {
		trans, cached, err = fn(ctx, preText)
	} else {
		trans, cached, err = s.translator.TranslateValidated(ctx, preText, func(raw string) error {
			if !pipeline.PlaceholdersPreserved(preText, r.post.Apply(raw)) {
				return errors.New("translation dropped [..]/{..} placeholders")
			}
			return nil
		})
	}
	if err != nil {
		return "", false, err
	}
	trans = r.post.Apply(trans)
	if !pipeline.PlaceholdersPreserved(preText, trans) {
		return "", cached, errors.New("translation dropped [..]/{..} placeholders")
	}
	return trans, cached, nil
}

// ShapeForEmbed applies wrapping, shaping and visual ordering with current settings.
func (s *Session) ShapeForEmbed(trans string) string {
	var start time.Time
	debugging := s.debugMode.Load()
	if debugging {
		start = time.Now()
	}
	cfg := s.embedCfg.Load()
	out := arabic.Process(trans, arabic.Options{
		MaxLineLength: cfg.MaxLineLength,
		ForceWrap:     cfg.LimitLineLength,
		DisplayMode:   arabic.DisplayMode(cfg.DisplayMode),
	})
	if debugging {
		s.recordTelemetry(SubArabicShaper, "arabic.Process", "trace", "shaped", time.Since(start).Microseconds(), false, map[string]any{
			"inRunes":  len([]rune(trans)),
			"outRunes": len([]rune(out)),
		})
	}
	return out
}

func (s *Session) handleEmbed(text string, tp hook.ThreadParam) {
	if strings.TrimSpace(text) != "" {
		s.setLastEmbed(text, tp)
	}
	if s.paused.Load() {
		if s.host != nil {
			s.host.EmbedCallback(tp, text, "")
		}
		return
	}
	start := time.Now()
	line := EmbedLine{Key: tp.Key(), Original: text}
	debugging := s.debugMode.Load()

	var (
		trans      string
		shaped     string
		cached     bool
		err        error
		preUs      int64
		transUs    int64
		postUs     int64
		shapeUs    int64
		callbackUs int64
		seq        uint64
	)

	if debugging {
		seq = s.debugSeq.Add(1)
	}

	r := s.rules.Load()
	t0 := time.Now()
	preText := r.pre.Apply(text)
	provName := s.translator.Active()
	var glossaryPrompt string
	if isLLMProvider(provName) {
		glossaryPrompt = r.glossary.PromptContext(preText)
	} else {
		preText = r.glossary.Apply(preText)
	}
	preUs = time.Since(t0).Microseconds()

	ecfg := s.embedCfg.Load()
	t1 := time.Now()
	if debugging && !s.debugRealTrans.Load() {
		trans = fmt.Sprintf("نص اختباري %d", seq)
	} else {
		timeout := time.Duration(ecfg.TimeoutMs) * time.Millisecond
		if timeout <= 0 {
			timeout = 2 * time.Second
		}
		if ecfg.NonBlocking {
			if timeout < 12*time.Second {
				timeout = 12 * time.Second
			}
		} else {
			// Leave headroom so the reply arrives before the game stops waiting.
			timeout = timeout * 9 / 10
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		if glossaryPrompt != "" {
			ctx = translator.WithGlossaryPrompt(ctx, glossaryPrompt)
		}
		trans, cached, err = s.translator.TranslateValidated(ctx, preText, func(raw string) error {
			if !pipeline.PlaceholdersPreserved(preText, r.post.Apply(raw)) {
				return errors.New("translation dropped [..]/{..} placeholders")
			}
			return nil
		})
		cancel()
	}
	transUs = time.Since(t1).Microseconds()

	if err != nil {
		line.Error = err.Error()
	} else {
		t2 := time.Now()
		trans = r.post.Apply(trans)
		if (!debugging || s.debugRealTrans.Load()) && !pipeline.PlaceholdersPreserved(preText, trans) {
			err = errors.New("translation dropped [..]/{..} placeholders")
			line.Error = err.Error()
		}
		postUs = time.Since(t2).Microseconds()

		if err == nil {
			t3 := time.Now()
			shaped = s.ShapeForEmbed(trans)
			shapeUs = time.Since(t3).Microseconds()
		}
	}

	// Always answer so the game thread is released immediately.
	tCb := time.Now()
	s.host.EmbedCallback(tp, text, shaped)
	callbackUs = time.Since(tCb).Microseconds()

	totalDur := time.Since(start)
	line.Translated, line.Shaped, line.Cached = trans, shaped, cached
	line.ElapsedMs = totalDur.Milliseconds()
	s.post(EventEmbedLine, line)

	timeoutMs := int64(ecfg.TimeoutMs)
	if timeoutMs <= 0 {
		timeoutMs = 2000
	}
	lvl := "trace"
	msg := "ok"
	isTimeout := false
	if err != nil {
		msg = err.Error()
		if strings.Contains(msg, "placeholders") {
			lvl = "warn"
		} else {
			lvl = "error"
		}
		isTimeout = isTimeoutMsg(msg)
	} else if !ecfg.NonBlocking && totalDur.Milliseconds() > (timeoutMs*80)/100 {
		lvl = "warn"
		msg = "embed latency > 80% of TimeoutMs"
	}
	if debugging || lvl == "warn" || lvl == "error" {
		glossaryMode := "Apply"
		if isLLMProvider(provName) {
			glossaryMode = "PromptContext"
		}
		s.recordTelemetry(SubPipelineCache, "Session.handleEmbed", lvl, msg, totalDur.Microseconds(), isTimeout, map[string]any{
			"key":          line.Key,
			"cached":       cached,
			"glossaryMode": glossaryMode,
			"preRulesUs":   preUs,
			"translateUs":  transUs,
			"postRulesUs":  postUs,
			"shapeUs":      shapeUs,
			"callbackUs":   callbackUs,
			"totalUs":      totalDur.Microseconds(),
			"origLen":      len([]rune(text)),
		})
	}

	if debugging {
		s.post(EventDebugTrace, DebugTrace{
			Seq:         seq,
			Time:        start.Format("15:04:05.000"),
			Key:         line.Key,
			OrigLen:     len([]rune(text)),
			Cached:      cached,
			PreRulesUs:  preUs,
			TranslateUs: transUs,
			PostRulesUs: postUs,
			ShapeUs:     shapeUs,
			CallbackUs:  callbackUs,
			TotalUs:     totalDur.Microseconds(),
			Error:       line.Error,
		})
	}
}

// ---- hook.Handler ----

// OnProcessConnect implements hook.Handler.
func (s *Session) OnProcessConnect(pid uint32) {
	s.mu.Lock()
	s.pids[pid] = true
	s.mu.Unlock()
	s.pushEmbedSettings(pid)
	s.log("info", fmt.Sprintf("process %d connected", pid))
	s.post(EventStatus, s.Status())
}

// OnProcessRemove implements hook.Handler.
func (s *Session) OnProcessRemove(pid uint32) {
	s.mu.Lock()
	delete(s.pids, pid)
	for k, h := range s.hooks {
		if h.PID == pid {
			delete(s.hooks, k)
			delete(s.embedding, k)
			delete(s.lastOutput, k)
		}
	}
	s.mu.Unlock()
	s.log("info", fmt.Sprintf("process %d disconnected", pid))
	s.post(EventStatus, s.Status())
}

// OnNewHook implements hook.Handler.
func (s *Session) OnNewHook(h hook.HookInfo) {
	s.mu.Lock()
	s.hooks[h.Key] = h
	view := HookView{HookInfo: h, Embedding: s.embedding[h.Key]}
	s.mu.Unlock()
	s.post(EventHookNew, view)
}

// OnRemoveHook implements hook.Handler.
func (s *Session) OnRemoveHook(h hook.HookInfo) {
	s.mu.Lock()
	delete(s.hooks, h.Key)
	delete(s.embedding, h.Key)
	delete(s.lastOutput, h.Key)
	s.mu.Unlock()
	s.post(EventHookDel, h.Key)
}

// OnOutput implements hook.Handler.
func (s *Session) OnOutput(h hook.HookInfo, text string) {
	if strings.TrimSpace(text) != "" {
		s.mu.Lock()
		s.lastOutput[h.Key] = text
		s.mu.Unlock()
	}
	s.post(EventHookText, map[string]string{"key": h.Key, "text": text})
}

// OnHostInfo implements hook.Handler.
func (s *Session) OnHostInfo(kind hook.HostInfo, msg string) {
	switch kind {
	case hook.HostInfoEngineType:
		s.mu.Lock()
		if !strings.HasPrefix(s.engine, "unity") || !strings.EqualFold(msg, "unity") {
			s.engine = strings.ToLower(msg)
		}
		pids := s.pidList()
		s.mu.Unlock()
		s.log("info", "engine: "+msg)
		go func() {
			for _, pid := range pids {
				s.pushEmbedSettings(pid) // Unity font dir depends on the engine
			}
		}()
		s.post(EventStatus, s.Status())
	case hook.HostInfoWarning, hook.HostInfoEmuWarning:
		if ev, ok := ParseNativeTelemetry(msg, "warn"); ok {
			s.recordTelemetry(ev.Subsystem, ev.Func, ev.Level, ev.Message, ev.DurationUs,
				isTimeoutMsg(ev.Message), ev.Fields)
			return
		}
		s.log("warn", msg)
	default:
		if ev, ok := ParseNativeTelemetry(msg, "info"); ok {
			s.recordTelemetry(ev.Subsystem, ev.Func, ev.Level, ev.Message, ev.DurationUs,
				isTimeoutMsg(ev.Message), ev.Fields)
			return
		}
		if strings.HasPrefix(msg, "[perf] ") {
			s.emitPerfTrace(strings.TrimPrefix(msg, "[perf] "))
		}
		s.log("info", msg)
	}
}

func (s *Session) emitPerfTrace(payload string) {
	// Expected format: "<metric>: <microseconds> us"
	parts := strings.SplitN(payload, ":", 2)
	if len(parts) != 2 {
		return
	}
	name := strings.TrimSpace(parts[0])
	valStr := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(parts[1]), "us"))
	us, err := strconv.ParseInt(valStr, 10, 64)
	if err != nil {
		return
	}
	s.post(EventDebugTrace, DebugTrace{
		Seq:     s.debugSeq.Add(1),
		Time:    time.Now().Format("15:04:05.000"),
		Key:     name,
		TotalUs: us,
	})
}

// OnHookInsert implements hook.Handler: re-enables saved embed hooks.
func (s *Session) OnHookInsert(pid uint32, addr uint64, hookCode string) {
	s.mu.Lock()
	if s.hookAddrs == nil {
		s.hookAddrs = map[string]map[uint32]uint64{}
	}
	byPid := s.hookAddrs[hookCode]
	if byPid == nil {
		byPid = map[uint32]uint64{}
		s.hookAddrs[hookCode] = byPid
	}
	byPid[pid] = addr
	path := s.gamePath
	s.mu.Unlock()
	if path == "" {
		return
	}
	go func() {
		for _, e := range s.store.Game(path).EmbedHooks {
			if e.HookCode != hookCode {
				continue
			}
			tp := hook.ThreadParam{ProcessID: pid, Addr: addr, Ctx: e.Ctx, Ctx2: e.Ctx2}
			s.host.UseEmbed(tp, true)
			s.mu.Lock()
			s.embedding[tp.Key()] = true
			s.mu.Unlock()
			s.log("info", "re-enabled embed for "+hookCode)
		}
	}()
}

// OnEmbedText implements hook.Handler.
func (s *Session) OnEmbedText(text string, tp hook.ThreadParam) {
	select {
	case s.embedCh <- embedJob{text: text, tp: tp}:
	default:
		if s.host != nil {
			s.host.EmbedCallback(tp, text, "")
		}
	}
}

func archName(is64 bool) string {
	if is64 {
		return "x64"
	}
	return "x86"
}

// ResolveRuntimeDir finds the runtime/ folder next to the executable or in the
// working directory (wails dev).
func ResolveRuntimeDir() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "runtime"), filepath.Join(dir, "..", "..", "runtime"))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "runtime"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "khutaf-hook64.dll")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "runtime"
}
