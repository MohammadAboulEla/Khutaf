//go:build windows

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"khutaf/internal/hook"
)

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
			delete(s.muted, k)
			delete(s.lastOutput, k)
			delete(s.hookStats, k)
		}
	}
	s.mu.Unlock()
	s.log("info", fmt.Sprintf("process %d disconnected", pid))
	s.post(EventStatus, s.Status())
}

// OnNewHook implements hook.Handler.
func (s *Session) OnNewHook(h hook.HookInfo) {
	nowMs := currentUnixMs()
	s.mu.Lock()
	s.hooks[h.Key] = h
	gamePath := s.gamePath
	hi := s.hookStats[h.Key]
	if hi == nil {
		hi = &hookIntel{}
		s.hookStats[h.Key] = hi
	}
	mk := hookMetaKey{hookID: h.Addr, ctx: h.Ctx, ctx2: h.Ctx2}
	if p, ok := s.pendingMeta[mk]; ok && hi.objectPath == "" {
		hi.setObjectPath(h, p, nowMs)
	} else if hi.category == "" {
		hi.category, hi.dialogueScore = classifyHook(h, hi, nowMs)
	}
	s.mu.Unlock()

	if gamePath != "" && s.isSavedHookMuted(gamePath, h) {
		s.mu.Lock()
		s.muted[h.Key] = true
		s.mu.Unlock()
	}

	s.mu.Lock()
	view := s.buildHookViewLocked(h, nowMs)
	s.mu.Unlock()
	s.post(EventHookNew, view)
}

// OnRemoveHook implements hook.Handler.
func (s *Session) OnRemoveHook(h hook.HookInfo) {
	s.mu.Lock()
	delete(s.hooks, h.Key)
	delete(s.embedding, h.Key)
	delete(s.muted, h.Key)
	delete(s.lastOutput, h.Key)
	delete(s.hookStats, h.Key)
	s.mu.Unlock()
	s.post(EventHookDel, h.Key)
}

// OnOutput implements hook.Handler.
func (s *Session) OnOutput(h hook.HookInfo, text string) {
	nowMs := currentUnixMs()
	var (
		cat      HookCategory = HookCatUnknown
		score    int
		hits     int
		rate     float64
		objPath  string
		speakerT string
	)
	if strings.TrimSpace(text) != "" {
		s.mu.Lock()
		s.lastOutput[h.Key] = text
		hi := s.hookStats[h.Key]
		if hi == nil {
			hi = &hookIntel{}
			s.hookStats[h.Key] = hi
		}
		recentDlg := s.lastDialogueKey != "" && s.lastDialogueKey != h.Key && (nowMs-s.lastDialogueMs) <= speakerCoChangeMs
		hi.recordText(h, text, nowMs, recentDlg)
		cat = hi.category
		score = hi.dialogueScore
		hits = hi.hitCount
		rate = hi.updatesPerSec(nowMs)
		objPath = hi.objectPath
		if cat == HookCatDialogue {
			s.lastDialogueMs = nowMs
			s.lastDialogueKey = h.Key
		} else if cat == HookCatSpeaker {
			speakerT = strings.TrimSpace(stripAngleTags(text))
		}
		s.mu.Unlock()
		if speakerT != "" {
			s.dialogueCtxMu.Lock()
			s.lastSpeaker = speakerT
			s.lastSpeakerAt = time.Now()
			s.dialogueCtxMu.Unlock()
		}
	}
	s.post(EventHookText, map[string]any{
		"key":           h.Key,
		"text":          text,
		"atMs":          nowMs,
		"category":      cat,
		"dialogueScore": score,
		"hitCount":      hits,
		"updatesPerSec": rate,
		"lastSeenMs":    nowMs,
		"objectPath":    objPath,
	})
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
		if hid, c1, c2, path, ok := parseNativeHookMeta(msg); ok {
			s.applyNativeHookMeta(hid, c1, c2, path)
			return
		}
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

func (s *Session) applyNativeHookMeta(hookID, ctx, ctx2 uint64, path string) {
	nowMs := currentUnixMs()
	mk := hookMetaKey{hookID: hookID, ctx: ctx, ctx2: ctx2}
	s.mu.Lock()
	if s.pendingMeta == nil {
		s.pendingMeta = map[hookMetaKey]string{}
	}
	if len(s.pendingMeta) >= 2048 {
		s.pendingMeta = map[hookMetaKey]string{}
	}
	s.pendingMeta[mk] = path
	var updated []HookView
	for _, h := range s.hooks {
		if h.Addr == hookID && h.Ctx == ctx && h.Ctx2 == ctx2 {
			hi := s.hookStats[h.Key]
			if hi == nil {
				hi = &hookIntel{}
				s.hookStats[h.Key] = hi
			}
			hi.setObjectPath(h, path, nowMs)
			updated = append(updated, s.buildHookViewLocked(h, nowMs))
		}
	}
	s.mu.Unlock()
	for _, v := range updated {
		s.post(EventHookNew, v)
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
