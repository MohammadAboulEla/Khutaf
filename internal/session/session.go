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
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	EventLog       = "log"
	EventStatus    = "session:status"
	EventHookNew   = "hook:new"
	EventHookDel   = "hook:remove"
	EventHookText  = "hook:output"
	EventEmbedLine = "embed:line"
)

// HookView is a hook as shown in the UI.
type HookView struct {
	hook.HookInfo
	Embedding bool `json:"embedding"`
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
	PIDs      []uint32 `json:"pids"`
	Provider  string   `json:"provider"`
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

type rules struct{ pre, post *pipeline.RuleSet }

// Session is the application core. Create with New.
type Session struct {
	store      *config.Store
	translator *translator.Manager
	host       *hook.Host
	hostErr    error
	runtimeDir string
	emit       Emitter
	events     chan func()

	rules atomic.Pointer[rules]

	mu        sync.Mutex
	gamePath  string
	engine    string // engine reported by LunaHook or detected from files
	is64      bool
	pids      map[uint32]bool
	hooks     map[string]hook.HookInfo
	embedding map[string]bool
}

// New loads the native host from runtimeDir (failure is non-fatal: the UI still
// works for previews and settings) and starts the event dispatcher.
func New(store *config.Store, runtimeDir string, emit Emitter) *Session {
	s := &Session{
		store:      store,
		translator: translator.NewManager(),
		runtimeDir: runtimeDir,
		emit:       emit,
		events:     make(chan func(), 4096),
		pids:       map[uint32]bool{},
		hooks:      map[string]hook.HookInfo{},
		embedding:  map[string]bool{},
	}
	go func() {
		for fn := range s.events {
			fn()
		}
	}()
	if err := s.applyConfig(store.Config()); err != nil {
		s.log("warn", err.Error())
	}
	host, err := hook.NewHost(runtimeDir)
	if err == nil {
		err = host.Start(s)
	}
	if err != nil {
		s.hostErr = err
		s.log("error", "hook host unavailable: "+err.Error())
	} else {
		s.host = host
		s.log("info", "hook host started ("+runtimeDir+")")
	}
	return s
}

// post queues UI work so native callbacks never block on the frontend.
func (s *Session) post(name string, data any) {
	select {
	case s.events <- func() { s.emit(name, data) }:
	default: // UI is overwhelmed; drop rather than stall the game thread
	}
}

func (s *Session) log(level, msg string) {
	s.post(EventLog, map[string]string{"level": level, "msg": msg, "time": time.Now().Format("15:04:05")})
}

// ---- configuration ----

// Config returns the current settings.
func (s *Session) Config() config.Config { return s.store.Config() }

// SaveConfig validates, applies and persists settings, then pushes embed
// settings to every connected process.
func (s *Session) SaveConfig(c config.Config) error {
	if err := s.applyConfig(c); err != nil {
		return err
	}
	if err := s.store.SaveConfig(c); err != nil {
		return err
	}
	s.mu.Lock()
	pids := s.pidList()
	s.mu.Unlock()
	for _, pid := range pids {
		s.pushEmbedSettings(pid)
	}
	s.post(EventStatus, s.Status())
	return nil
}

func (s *Session) applyConfig(c config.Config) error {
	pre, err := pipeline.Compile(c.Pipeline.PreRules)
	if err != nil {
		return fmt.Errorf("pre-processing: %w", err)
	}
	post, err := pipeline.Compile(c.Pipeline.PostRules)
	if err != nil {
		return fmt.Errorf("post-processing: %w", err)
	}
	s.rules.Store(&rules{pre: pre, post: post})
	s.translator.Configure(c.Translator)
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
		PIDs:      s.pidList(),
		Provider:  s.translator.Active(),
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
		out = append(out, HookView{HookInfo: h, Embedding: s.embedding[k]})
	}
	return out
}

// ListProcesses returns attachable windowed processes.
func (s *Session) ListProcesses() ([]hook.ProcessInfo, error) { return hook.ListProcesses() }

// ---- attach / detach ----

// Attach connects to every running instance of pid's executable and injects LunaHook.
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
	pids := hook.PIDsByPath(path)
	if len(pids) == 0 {
		pids = []uint32{pid}
	}

	s.Detach()
	s.mu.Lock()
	s.gamePath = path
	s.is64 = is64
	s.engine = hook.DetectEngine(path)
	s.mu.Unlock()

	cfg := s.store.Config().Hook
	s.host.Settings(hook.HostSettings{
		FlushDelayMs:   int32(cfg.FlushDelayMs),
		Codepage:       int32(cfg.Codepage),
		MaxBufferSize:  int32(cfg.MaxBufferSize),
		MaxHistorySize: int32(cfg.MaxHistorySize),
	})

	var needInject []uint32
	for _, p := range pids {
		s.host.ConnectProcess(p)
		if s.host.CheckIfNeedInject(p) {
			needInject = append(needInject, p)
		}
	}
	s.log("info", fmt.Sprintf("attaching to %s (pids %v, %s)", filepath.Base(path), pids, archName(is64)))
	if err := hook.Inject(s.runtimeDir, needInject, is64); err != nil {
		s.log("error", err.Error())
		return err
	}
	s.post(EventStatus, s.Status())
	return nil
}

// Detach unhooks all connected processes and clears hook state.
func (s *Session) Detach() {
	s.mu.Lock()
	pids := s.pidList()
	s.pids = map[uint32]bool{}
	s.hooks = map[string]hook.HookInfo{}
	s.embedding = map[string]bool{}
	s.gamePath = ""
	s.engine = ""
	s.mu.Unlock()
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
	if ok {
		if enabled {
			s.embedding[key] = true
		} else {
			delete(s.embedding, key)
		}
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown hook %s", key)
	}
	s.host.UseEmbed(h.TP, enabled)
	return s.store.UpdateGame(path, func(g *config.GameProfile) {
		kept := g.EmbedHooks[:0]
		for _, e := range g.EmbedHooks {
			if !(e.HookCode == h.HookCode && e.Ctx == h.Ctx && e.Ctx2 == h.Ctx2) {
				kept = append(kept, e)
			}
		}
		g.EmbedHooks = kept
		if enabled {
			g.EmbedHooks = append(g.EmbedHooks, config.EmbedHook{HookCode: h.HookCode, HookName: h.HookName, Ctx: h.Ctx, Ctx2: h.Ctx2})
		}
		if g.Title == "" {
			g.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	})
}

// InsertHookCode inserts a manual H-code into all connected processes.
func (s *Session) InsertHookCode(code string) error {
	if s.host == nil {
		return errors.New("hook host unavailable")
	}
	s.mu.Lock()
	pids := s.pidList()
	s.mu.Unlock()
	if len(pids) == 0 {
		return errors.New("not attached")
	}
	for _, pid := range pids {
		s.host.InsertHookCode(pid, code)
	}
	return nil
}

func (s *Session) pushEmbedSettings(pid uint32) {
	if s.host == nil {
		return
	}
	cfg := s.store.Config().Embed
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
	s.host.SettingsEx(pid, hook.EmbedSettings{
		WaitTimeMs:       uint32(cfg.TimeoutMs),
		FontFamily:       font,
		DisplayMode:      hook.DisplayMode(cfg.DisplayMode),
		ClearText:        cfg.ClearText,
		ChangeFontSize:   cfg.ChangeFontSize,
		FontSizeRelative: cfg.FontSizeRelative,
		UnityFontDir:     unityDir,
	})
}

func (s *Session) unityFontDir() string {
	dir := filepath.Join(s.runtimeDir, "TMP_Font_AssetBundles")
	if matches, _ := filepath.Glob(filepath.Join(dir, "arialuni_sdf*")); len(matches) > 0 {
		return dir
	}
	return ""
}

// ---- translation ----

// Translate runs the full pipeline (pre-rules, translator, post-rules,
// placeholder check) and returns the logical (unshaped) translation.
func (s *Session) Translate(ctx context.Context, text string) (string, bool, error) {
	r := s.rules.Load()
	trans, cached, err := s.translator.Translate(ctx, r.pre.Apply(text))
	if err != nil {
		return "", false, err
	}
	trans = r.post.Apply(trans)
	if !pipeline.PlaceholdersPreserved(text, trans) {
		return "", cached, errors.New("translation dropped [..]/{..} placeholders")
	}
	return trans, cached, nil
}

// ShapeForEmbed applies wrapping, shaping and visual ordering with current settings.
func (s *Session) ShapeForEmbed(trans string) string {
	cfg := s.store.Config().Embed
	return arabic.Process(trans, arabic.Options{
		MaxLineLength: cfg.MaxLineLength,
		ForceWrap:     cfg.LimitLineLength,
		DisplayMode:   arabic.DisplayMode(cfg.DisplayMode),
	})
}

func (s *Session) handleEmbed(text string, tp hook.ThreadParam) {
	start := time.Now()
	timeout := time.Duration(s.store.Config().Embed.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	// Leave headroom so the reply arrives before the game stops waiting.
	ctx, cancel := context.WithTimeout(context.Background(), timeout*9/10)
	defer cancel()

	line := EmbedLine{Key: tp.Key(), Original: text}
	trans, cached, err := s.Translate(ctx, text)
	shaped := ""
	if err != nil {
		line.Error = err.Error()
	} else {
		shaped = s.ShapeForEmbed(trans)
	}
	// Always answer so the game thread is released immediately.
	s.host.EmbedCallback(tp, text, shaped)

	line.Translated, line.Shaped, line.Cached = trans, shaped, cached
	line.ElapsedMs = time.Since(start).Milliseconds()
	s.post(EventEmbedLine, line)
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
	s.mu.Unlock()
	s.post(EventHookDel, h.Key)
}

// OnOutput implements hook.Handler.
func (s *Session) OnOutput(h hook.HookInfo, text string) {
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
		for _, pid := range pids {
			s.pushEmbedSettings(pid) // Unity font dir depends on the engine
		}
		s.post(EventStatus, s.Status())
	case hook.HostInfoWarning, hook.HostInfoEmuWarning:
		s.log("warn", msg)
	default:
		s.log("info", msg)
	}
}

// OnHookInsert implements hook.Handler: re-enables saved embed hooks.
func (s *Session) OnHookInsert(pid uint32, addr uint64, hookCode string) {
	s.mu.Lock()
	path := s.gamePath
	s.mu.Unlock()
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
}

// OnEmbedText implements hook.Handler.
func (s *Session) OnEmbedText(text string, tp hook.ThreadParam) {
	go s.handleEmbed(text, tp)
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
		if _, err := os.Stat(filepath.Join(c, "KhutafHost64.dll")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "runtime"
}
