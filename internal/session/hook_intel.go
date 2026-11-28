//go:build windows

package session

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"khutaf/internal/config"
	"khutaf/internal/hook"
)

// HookCategory classifies a discovered text hook by its observed role in-game.
type HookCategory string

const (
	HookCatDialogue HookCategory = "dialogue"
	HookCatSpeaker  HookCategory = "speaker"
	HookCatUI       HookCategory = "ui"
	HookCatNoise    HookCategory = "noise"
	HookCatUnknown  HookCategory = "unknown"
)

const (
	maxHookHistorySamples = 15
	rateWindowMs          = 10_000
	speakerCoChangeMs     = 350
)

// HookSample is one recent unique line captured on a hook (in-memory only).
type HookSample struct {
	Text string `json:"text"`
	AtMs int64  `json:"atMs"`
}

// HookPresetResult reports changes performed by ApplyHookPreset or RestoreEmbedHooks.
type HookPresetResult struct {
	Preset       string   `json:"preset"`
	EnabledNow   int      `json:"enabledNow"`
	DisabledNow  int      `json:"disabledNow"`
	TotalActive  int      `json:"totalActive"`
	PreviousKeys []string `json:"previousKeys"`
}

type hookMetaKey struct {
	hookID uint64
	ctx    uint64
	ctx2   uint64
}

type hookIntel struct {
	objectPath     string
	history        []HookSample
	timestamps     []int64
	hitCount       int
	lastSeenMs     int64
	totalRunes     int
	maxRunes       int
	punctHits      int
	digitHeavyHits int
	speakerCoHits  int
	category       HookCategory
	dialogueScore  int
}

func (hi *hookIntel) updatesPerSec(nowMs int64) float64 {
	if len(hi.timestamps) == 0 {
		return 0
	}
	cutoff := nowMs - rateWindowMs
	recent := 0
	for _, ts := range hi.timestamps {
		if ts >= cutoff {
			recent++
		}
	}
	if recent <= 1 {
		return 0
	}
	oldest := hi.timestamps[len(hi.timestamps)-recent]
	spanSec := float64(nowMs-oldest) / 1000.0
	if spanSec < 1.0 {
		spanSec = 1.0
	}
	maxSpan := float64(rateWindowMs) / 1000.0
	if spanSec > maxSpan {
		spanSec = maxSpan
	}
	rate := float64(recent) / spanSec
	return math.Round(rate*10) / 10
}

func (hi *hookIntel) recordText(h hook.HookInfo, text string, nowMs int64, recentDialogue bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	hi.hitCount++
	hi.lastSeenMs = nowMs

	// Maintain sliding 10-second timestamp window for churn detection.
	cutoff := nowMs - rateWindowMs
	keep := hi.timestamps[:0]
	for _, ts := range hi.timestamps {
		if ts >= cutoff {
			keep = append(keep, ts)
		}
	}
	hi.timestamps = append(keep, nowMs)
	if len(hi.timestamps) > 64 {
		hi.timestamps = hi.timestamps[len(hi.timestamps)-64:]
	}

	// Update per-hook ring buffer of unique recent lines (newest first).
	found := -1
	for i, sample := range hi.history {
		if sample.Text == trimmed {
			found = i
			break
		}
	}
	if found >= 0 {
		copy(hi.history[1:found+1], hi.history[0:found])
		hi.history[0] = HookSample{Text: trimmed, AtMs: nowMs}
	} else {
		if len(hi.history) < maxHookHistorySamples {
			hi.history = append(hi.history, HookSample{})
		}
		copy(hi.history[1:], hi.history[0:len(hi.history)-1])
		hi.history[0] = HookSample{Text: trimmed, AtMs: nowMs}
	}

	runes := visibleRuneCount(trimmed)
	hi.totalRunes += runes
	if runes > hi.maxRunes {
		hi.maxRunes = runes
	}
	if hasSentencePunctuation(trimmed) {
		hi.punctHits++
	}
	if isDigitOrSymbolHeavy(trimmed) {
		hi.digitHeavyHits++
	}
	if recentDialogue && isSpeakerCandidate(trimmed) {
		hi.speakerCoHits++
	}

	hi.category, hi.dialogueScore = classifyHook(h, hi, nowMs)
}

func (hi *hookIntel) setObjectPath(h hook.HookInfo, path string, nowMs int64) {
	hi.objectPath = path
	hi.category, hi.dialogueScore = classifyHook(h, hi, nowMs)
}

// parseNativeHookMeta parses `[meta|<hook_id>|<ctx_hex>|<ctx2_hex>] <path>`.
func parseNativeHookMeta(msg string) (hookID, ctx, ctx2 uint64, path string, ok bool) {
	if !strings.HasPrefix(msg, "[meta|") {
		return 0, 0, 0, "", false
	}
	end := strings.IndexByte(msg, ']')
	if end < 0 {
		return 0, 0, 0, "", false
	}
	parts := strings.Split(msg[6:end], "|")
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	hid, err1 := strconv.ParseUint(parts[0], 10, 64)
	c1, err2 := strconv.ParseUint(parts[1], 16, 64)
	c2, err3 := strconv.ParseUint(parts[2], 16, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, "", false
	}
	p := strings.TrimSpace(msg[end+1:])
	if p == "" {
		return 0, 0, 0, "", false
	}
	return hid, c1, c2, p, true
}

func visibleRuneCount(s string) int {
	inTag := false
	n := 0
	for _, r := range s {
		if inTag {
			if r == '>' {
				inTag = false
			}
			continue
		}
		if r == '<' {
			inTag = true
			continue
		}
		n++
	}
	return n
}

func wordCount(s string) int {
	return len(strings.Fields(stripAngleTags(s)))
}

func stripAngleTags(s string) string {
	if !strings.ContainsRune(s, '<') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		if inTag {
			if r == '>' {
				inTag = false
			}
			continue
		}
		if r == '<' {
			inTag = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hasSentencePunctuation(s string) bool {
	plain := stripAngleTags(s)
	for _, r := range plain {
		switch r {
		case '.', '?', '!', ',', ';', '—', '…', '"', '“', '”', '،', '؟', '。', '！', '？', '、':
			return true
		}
	}
	return false
}

func isDigitOrSymbolHeavy(s string) bool {
	plain := stripAngleTags(s)
	letters, nonLetters := 0, 0
	for _, r := range plain {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsLetter(r) {
			letters++
		} else {
			nonLetters++
		}
	}
	total := letters + nonLetters
	if total == 0 {
		return true
	}
	if letters < 2 {
		return true
	}
	return float64(nonLetters)/float64(total) >= 0.48
}

func isSpeakerCandidate(s string) bool {
	plain := strings.TrimSpace(stripAngleTags(s))
	runes := len([]rune(plain))
	if runes < 2 || runes > 28 {
		return false
	}
	if strings.ContainsAny(plain, ".?!\n\r") {
		return false
	}
	words := strings.Fields(plain)
	if len(words) == 0 || len(words) > 3 {
		return false
	}
	letters := 0
	for _, r := range plain {
		if unicode.IsDigit(r) {
			return false
		}
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 2
}

var (
	dialoguePathKeywords = []string{
		"dialog", "dialogue", "subtitle", "speech", "narration", "narrative",
		"story", "caption", "convo", "conversation", "talk", "chat",
		"bodytext", "dialogtext", "linetext", "message", "textbox", "bubble",
	}
	speakerPathKeywords = []string{
		"speaker", "charname", "charactername", "actorname", "talker",
		"nameplate", "nametext", "speakername", "portraitname", "npcname",
	}
	uiPathKeywords = []string{
		"menu", "button", "btn", "tab", "header", "setting", "option",
		"inventory", "tooltip", "quest", "shop", "popup", "confirm", "cancel",
		"label", "title", "prompt", "hint", "keybind", "pause", "credits",
	}
	noisePathKeywords = []string{
		"fps", "ping", "timer", "clock", "counter", "score", "ammo",
		"stamina", "coord", "version", "build", "debug", "money", "gold",
		"coin", "damage", "combo", "speed", "distance", "health", "mana",
	}
)

func containsAnyToken(s string, tokens []string) bool {
	for _, tok := range tokens {
		if strings.Contains(s, tok) {
			return true
		}
	}
	return false
}

func classifyHook(h hook.HookInfo, hi *hookIntel, nowMs int64) (HookCategory, int) {
	codeLow := strings.ToLower(h.HookCode + " " + h.HookName)
	pathLow := strings.ToLower(hi.objectPath)

	isStaticUI := strings.Contains(codeLow, "staticui")
	isTextAnim := strings.Contains(codeLow, "textanimator") || strings.Contains(codeLow, "naninovel")

	score := 35
	if isTextAnim {
		score += 40
	}
	if isStaticUI {
		score -= 25
	}

	pathSpeaker := pathLow != "" && containsAnyToken(pathLow, speakerPathKeywords)
	pathDialogue := pathLow != "" && containsAnyToken(pathLow, dialoguePathKeywords)
	pathNoise := pathLow != "" && containsAnyToken(pathLow, noisePathKeywords)
	pathUI := pathLow != "" && containsAnyToken(pathLow, uiPathKeywords)

	if pathDialogue {
		score += 38
	}
	if pathSpeaker {
		score += 20
	}
	if pathUI && !pathDialogue {
		score -= 18
	}
	if pathNoise && !pathDialogue {
		score -= 40
	}

	avgRunes := 0.0
	punctRatio := 0.0
	digitRatio := 0.0
	avgWords := 0.0
	if hi.hitCount > 0 {
		avgRunes = float64(hi.totalRunes) / float64(hi.hitCount)
		punctRatio = float64(hi.punctHits) / float64(hi.hitCount)
		digitRatio = float64(hi.digitHeavyHits) / float64(hi.hitCount)
	}
	if len(hi.history) > 0 {
		totalW := 0
		for _, sample := range hi.history {
			totalW += wordCount(sample.Text)
		}
		avgWords = float64(totalW) / float64(len(hi.history))
	}

	rate := hi.updatesPerSec(nowMs)
	highChurn := rate >= 3.5 && hi.hitCount >= 5 && avgRunes < 36

	if hi.maxRunes >= 42 || avgRunes >= 28 {
		score += 30
	} else if hi.maxRunes >= 24 || avgRunes >= 18 {
		score += 14
	} else if hi.hitCount > 0 && hi.maxRunes <= 14 && avgWords <= 2.2 {
		score -= 16
	}

	if avgWords >= 5 {
		score += 18
	} else if avgWords >= 3.5 {
		score += 8
	}
	if punctRatio >= 0.35 && hi.maxRunes >= 16 {
		score += 15
	}
	if digitRatio >= 0.55 && hi.hitCount >= 1 {
		score -= 42
	}
	if highChurn {
		score -= 45
	}

	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}

	// Priority classification decisions.
	if isStaticUI {
		return HookCatUI, min(score, 40)
	}
	if highChurn || (digitRatio >= 0.65 && hi.hitCount >= 1 && !pathDialogue) || (pathNoise && hi.maxRunes < 30) {
		return HookCatNoise, min(score, 15)
	}
	if (pathSpeaker && hi.maxRunes <= 32) || (hi.speakerCoHits >= 2 && hi.maxRunes <= 28 && !pathDialogue && !pathUI) {
		return HookCatSpeaker, max(score, 68)
	}
	if isTextAnim || score >= 60 {
		return HookCatDialogue, max(score, 62)
	}
	if pathUI || (hi.hitCount > 0 && hi.maxRunes <= 22 && avgWords <= 3.0 && punctRatio < 0.25) {
		return HookCatUI, score
	}
	if hi.hitCount == 0 && pathLow == "" {
		return HookCatUnknown, score
	}
	return HookCatUI, score
}

func (s *Session) buildHookViewLocked(h hook.HookInfo, nowMs int64) HookView {
	v := HookView{
		HookInfo:  h,
		Embedding: s.embedding[h.Key],
		Muted:     s.muted[h.Key],
		LastText:  s.lastOutput[h.Key],
		Category:  HookCatUnknown,
	}
	if hi, ok := s.hookStats[h.Key]; ok && hi != nil {
		v.ObjectPath = hi.objectPath
		v.Category = hi.category
		v.DialogueScore = hi.dialogueScore
		v.HitCount = hi.hitCount
		v.UpdatesPerSec = hi.updatesPerSec(nowMs)
		v.LastSeenMs = hi.lastSeenMs
		if len(hi.history) > 0 {
			v.History = make([]HookSample, len(hi.history))
			copy(v.History, hi.history)
		}
	} else {
		cat, score := classifyHook(h, &hookIntel{}, nowMs)
		v.Category = cat
		v.DialogueScore = score
	}
	return v
}

func (s *Session) isSavedHookMuted(gamePath string, h hook.HookInfo) bool {
	if gamePath == "" {
		return false
	}
	for _, m := range s.store.Game(gamePath).MutedHooks {
		if m.HookCode == h.HookCode && m.Ctx == h.Ctx && m.Ctx2 == h.Ctx2 {
			return true
		}
	}
	return false
}

// SetHookMuted mutes/ignores or unmutes a live hook and persists it in the game profile.
func (s *Session) SetHookMuted(key string, muted bool) error {
	s.mu.Lock()
	h, ok := s.hooks[key]
	path := s.gamePath
	wasEmbedding := s.embedding[key]
	var objPath, sample string
	if ok {
		if muted {
			s.muted[key] = true
			delete(s.embedding, key)
		} else {
			delete(s.muted, key)
		}
		sample = truncateSample(s.lastOutput[key], 120)
		if hi := s.hookStats[key]; hi != nil {
			objPath = hi.objectPath
		}
	}
	nowMs := currentUnixMs()
	var view HookView
	if ok {
		view = s.buildHookViewLocked(h, nowMs)
	}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown hook %s", key)
	}
	if muted && wasEmbedding && s.host != nil {
		s.host.UseEmbed(h.TP, false)
	}
	if path != "" {
		if err := s.store.UpdateGame(path, func(g *config.GameProfile) {
			keptMuted := g.MutedHooks[:0]
			for _, m := range g.MutedHooks {
				if !(m.HookCode == h.HookCode && m.Ctx == h.Ctx && m.Ctx2 == h.Ctx2) {
					keptMuted = append(keptMuted, m)
				}
			}
			g.MutedHooks = keptMuted
			if muted {
				g.MutedHooks = append(g.MutedHooks, config.EmbedHook{
					HookCode: h.HookCode,
					HookName: h.HookName,
					Ctx:      h.Ctx,
					Ctx2:     h.Ctx2,
					Sample:   sample,
					Path:     objPath,
				})
				// Also remove from saved EmbedHooks when muted.
				keptEmb := g.EmbedHooks[:0]
				for _, e := range g.EmbedHooks {
					if !(e.HookCode == h.HookCode && e.Ctx == h.Ctx && e.Ctx2 == h.Ctx2) {
						keptEmb = append(keptEmb, e)
					}
				}
				g.EmbedHooks = keptEmb
			}
			if g.Title == "" {
				g.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			}
		}); err != nil {
			return err
		}
	}
	s.post(EventHookNew, view)
	return nil
}

// ApplyHookPreset applies a one-click selection preset ("dialogue", "dialogue_ui", "disable_all")
// and returns the previously enabled keys so the UI can offer instant Undo.
func (s *Session) ApplyHookPreset(preset string) (HookPresetResult, error) {
	if s.host == nil {
		return HookPresetResult{}, errors.New("hook host unavailable")
	}
	nowMs := currentUnixMs()
	s.mu.Lock()
	if len(s.pids) == 0 {
		s.mu.Unlock()
		return HookPresetResult{}, errors.New("no game attached")
	}
	prevKeys := make([]string, 0, len(s.embedding))
	for k, on := range s.embedding {
		if on {
			prevKeys = append(prevKeys, k)
		}
	}
	views := make([]HookView, 0, len(s.hooks))
	for _, h := range s.hooks {
		views = append(views, s.buildHookViewLocked(h, nowMs))
	}
	s.mu.Unlock()

	want := map[string]bool{}
	switch preset {
	case "dialogue":
		for _, v := range views {
			if !v.Embeddable || v.Muted {
				continue
			}
			if v.Category == HookCatDialogue || v.Category == HookCatSpeaker {
				want[v.Key] = true
			}
		}
		// Fallback to highest-scoring non-noise Unity hook with text if nothing hit dialogue threshold yet.
		if len(want) == 0 {
			bestKey := ""
			bestScore := -1
			for _, v := range views {
				if !v.Embeddable || v.Muted || v.Category == HookCatNoise || strings.TrimSpace(v.LastText) == "" {
					continue
				}
				if v.DialogueScore > bestScore {
					bestScore = v.DialogueScore
					bestKey = v.Key
				}
			}
			if bestKey != "" {
				want[bestKey] = true
			}
		}
	case "dialogue_ui":
		for _, v := range views {
			if !v.Embeddable || v.Muted || v.Category == HookCatNoise {
				continue
			}
			if v.Category == HookCatDialogue || v.Category == HookCatSpeaker || v.Category == HookCatUI {
				want[v.Key] = true
			}
		}
	case "disable_all":
		// empty want disables all
	default:
		return HookPresetResult{}, fmt.Errorf("unknown hook preset %q", preset)
	}

	return s.syncDesiredEmbedKeys(preset, prevKeys, views, want)
}

// RestoreEmbedHooks restores a previously snapshotted set of enabled hook keys (Undo).
func (s *Session) RestoreEmbedHooks(keys []string) (HookPresetResult, error) {
	if s.host == nil {
		return HookPresetResult{}, errors.New("hook host unavailable")
	}
	nowMs := currentUnixMs()
	s.mu.Lock()
	if len(s.pids) == 0 {
		s.mu.Unlock()
		return HookPresetResult{}, errors.New("no game attached")
	}
	prevKeys := make([]string, 0, len(s.embedding))
	for k, on := range s.embedding {
		if on {
			prevKeys = append(prevKeys, k)
		}
	}
	views := make([]HookView, 0, len(s.hooks))
	for _, h := range s.hooks {
		views = append(views, s.buildHookViewLocked(h, nowMs))
	}
	s.mu.Unlock()

	want := make(map[string]bool, len(keys))
	for _, k := range keys {
		want[k] = true
	}
	return s.syncDesiredEmbedKeys("restore", prevKeys, views, want)
}

func (s *Session) syncDesiredEmbedKeys(preset string, prevKeys []string, views []HookView, want map[string]bool) (HookPresetResult, error) {
	enabledNow, disabledNow, totalActive := 0, 0, 0
	for _, v := range views {
		if !v.Embeddable {
			continue
		}
		target := want[v.Key] && !v.Muted
		if target {
			totalActive++
		}
		if target == v.Embedding {
			continue
		}
		if err := s.SetEmbed(v.Key, target); err != nil {
			return HookPresetResult{}, err
		}
		if target {
			enabledNow++
		} else {
			disabledNow++
		}
	}
	return HookPresetResult{
		Preset:       preset,
		EnabledNow:   enabledNow,
		DisabledNow:  disabledNow,
		TotalActive:  totalActive,
		PreviousKeys: prevKeys,
	}, nil
}
