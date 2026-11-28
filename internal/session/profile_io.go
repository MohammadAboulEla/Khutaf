//go:build windows

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"khutaf/internal/config"
	"khutaf/internal/hook"
	"khutaf/internal/pipeline"
)

const shareableProfileVersion = 1

// ShareableGameProfile is a sanitized per-game profile safe to share across machines.
// Local filesystem paths and translator credentials are intentionally omitted.
type ShareableGameProfile struct {
	FormatVersion int                     `json:"formatVersion"`
	AppVersion    string                  `json:"appVersion,omitempty"`
	ExportedAt    string                  `json:"exportedAt"`
	Title         string                  `json:"title"`
	ExeName       string                  `json:"exeName"`
	EmbedHooks    []config.EmbedHook      `json:"embedHooks,omitempty"`
	MutedHooks    []config.EmbedHook      `json:"mutedHooks,omitempty"`
	Pipeline      config.PipelineSettings `json:"pipeline,omitempty"`
	EmbedOverride *config.EmbedSettings   `json:"embedOverride,omitempty"`
}

// ImportProfileResult summarizes what was merged from an imported profile file.
type ImportProfileResult struct {
	Title                string `json:"title"`
	HooksAdded           int    `json:"hooksAdded"`
	MutedAdded           int    `json:"mutedAdded"`
	GlossaryAdded        int    `json:"glossaryAdded"`
	RulesAdded           int    `json:"rulesAdded"`
	EmbedOverrideApplied bool   `json:"embedOverrideApplied"`
}

// AddGlossaryTerm adds or updates a glossary entry either in the currently attached game's
// profile (when scopeGame is true and a game is attached) or in the global glossary.
func (s *Session) AddGlossaryTerm(source, target string, caseSensitive, scopeGame bool) error {
	src := strings.TrimSpace(source)
	tgt := strings.TrimSpace(target)
	if src == "" || tgt == "" {
		return errors.New("both source term and Arabic translation are required")
	}
	entry := config.GlossaryEntry{
		Enabled:       true,
		Source:        src,
		Target:        tgt,
		CaseSensitive: caseSensitive,
	}

	s.mu.Lock()
	gamePath := s.gamePath
	s.mu.Unlock()

	if scopeGame && gamePath != "" {
		if err := s.store.UpdateGame(gamePath, func(g *config.GameProfile) {
			if g.Title == "" {
				g.Title = strings.TrimSuffix(filepath.Base(gamePath), filepath.Ext(gamePath))
			}
			g.Pipeline.Glossary = upsertGlossaryEntry(g.Pipeline.Glossary, entry)
		}); err != nil {
			return err
		}
		return s.rebuildRules(s.store.Config().Pipeline, gamePath)
	}

	cfg := s.store.Config()
	cfg.Pipeline.Glossary = upsertGlossaryEntry(cfg.Pipeline.Glossary, entry)
	return s.SaveConfig(cfg)
}

func upsertGlossaryEntry(list []config.GlossaryEntry, entry config.GlossaryEntry) []config.GlossaryEntry {
	out := make([]config.GlossaryEntry, len(list))
	copy(out, list)
	for i := range out {
		if strings.EqualFold(strings.TrimSpace(out[i].Source), entry.Source) {
			out[i] = entry
			return out
		}
	}
	return append(out, entry)
}

// ExportGameProfileData serializes a sanitized ShareableGameProfile JSON for exePath.
func (s *Session) ExportGameProfileData(exePath, appVersion string) ([]byte, string, error) {
	if strings.TrimSpace(exePath) == "" {
		return nil, "", errors.New("game executable path is required")
	}
	prof := s.store.Game(exePath)
	title := strings.TrimSpace(prof.Title)
	exeName := filepath.Base(exePath)
	if title == "" {
		title = strings.TrimSuffix(exeName, filepath.Ext(exeName))
	}
	payload := ShareableGameProfile{
		FormatVersion: shareableProfileVersion,
		AppVersion:    appVersion,
		ExportedAt:    time.Now().UTC().Format(time.RFC3339),
		Title:         title,
		ExeName:       exeName,
		EmbedHooks:    prof.EmbedHooks,
		MutedHooks:    prof.MutedHooks,
		Pipeline:      prof.Pipeline,
		EmbedOverride: prof.EmbedOverride,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, "", err
	}
	filename := sanitizeFilename(title) + ".khutaf.json"
	return raw, filename, nil
}

// ImportGameProfileData validates and merges a ShareableGameProfile JSON into targetExePath.
func (s *Session) ImportGameProfileData(targetExePath string, data []byte) (ImportProfileResult, error) {
	if strings.TrimSpace(targetExePath) == "" {
		return ImportProfileResult{}, errors.New("target game path is required")
	}
	var in ShareableGameProfile
	if err := json.Unmarshal(data, &in); err != nil {
		return ImportProfileResult{}, fmt.Errorf("invalid profile file: %w", err)
	}
	if in.FormatVersion < 1 || in.FormatVersion > shareableProfileVersion {
		return ImportProfileResult{}, fmt.Errorf("unsupported profile format version %d", in.FormatVersion)
	}
	if _, err := pipeline.Compile(in.Pipeline.PreRules); err != nil {
		return ImportProfileResult{}, fmt.Errorf("profile pre-rules invalid: %w", err)
	}
	if _, err := pipeline.Compile(in.Pipeline.PostRules); err != nil {
		return ImportProfileResult{}, fmt.Errorf("profile post-rules invalid: %w", err)
	}

	var res ImportProfileResult
	res.Title = in.Title

	var normEmbed *config.EmbedSettings
	if in.EmbedOverride != nil {
		e := normalizeEmbed(*in.EmbedOverride)
		normEmbed = &e
	}

	if err := s.store.UpdateGame(targetExePath, func(g *config.GameProfile) {
		if g.Title == "" {
			if strings.TrimSpace(in.Title) != "" {
				g.Title = strings.TrimSpace(in.Title)
			} else {
				g.Title = strings.TrimSuffix(filepath.Base(targetExePath), filepath.Ext(targetExePath))
			}
		}

		// Merge embed hooks without duplicates.
		for _, eh := range in.EmbedHooks {
			if strings.TrimSpace(eh.HookCode) == "" {
				continue
			}
			if !hasSavedHook(g.EmbedHooks, eh) {
				g.EmbedHooks = append(g.EmbedHooks, eh)
				res.HooksAdded++
			}
		}

		// Merge muted hooks without duplicates.
		for _, mh := range in.MutedHooks {
			if strings.TrimSpace(mh.HookCode) == "" {
				continue
			}
			if !hasSavedHook(g.MutedHooks, mh) {
				g.MutedHooks = append(g.MutedHooks, mh)
				res.MutedAdded++
			}
		}

		// Merge pre/post rules without exact duplicates.
		for _, r := range in.Pipeline.PreRules {
			if !hasRule(g.Pipeline.PreRules, r) {
				g.Pipeline.PreRules = append(g.Pipeline.PreRules, r)
				res.RulesAdded++
			}
		}
		for _, r := range in.Pipeline.PostRules {
			if !hasRule(g.Pipeline.PostRules, r) {
				g.Pipeline.PostRules = append(g.Pipeline.PostRules, r)
				res.RulesAdded++
			}
		}

		// Merge glossary terms (case-insensitive by source).
		for _, gl := range in.Pipeline.Glossary {
			if strings.TrimSpace(gl.Source) == "" || strings.TrimSpace(gl.Target) == "" {
				continue
			}
			before := len(g.Pipeline.Glossary)
			g.Pipeline.Glossary = upsertGlossaryEntry(g.Pipeline.Glossary, gl)
			if len(g.Pipeline.Glossary) > before {
				res.GlossaryAdded++
			}
		}

		if normEmbed != nil {
			g.EmbedOverride = normEmbed
			res.EmbedOverrideApplied = true
		}
	}); err != nil {
		return ImportProfileResult{}, err
	}

	// If this game is currently attached, apply rules, overrides, and newly imported hooks live.
	s.mu.Lock()
	attachedMatch := s.gamePath != "" && strings.EqualFold(filepath.Clean(s.gamePath), filepath.Clean(targetExePath))
	pids := s.pidList()
	s.mu.Unlock()

	if attachedMatch {
		_ = s.applyConfig(s.store.Config())
		for _, pid := range pids {
			s.pushEmbedSettings(pid)
		}
		s.applyImportedHooksLive(targetExePath)
		s.post(EventStatus, s.Status())
	}

	return res, nil
}

func (s *Session) applyImportedHooksLive(exePath string) {
	prof := s.store.Game(exePath)
	nowMs := currentUnixMs()
	s.mu.Lock()
	var enableList []hook.HookInfo
	var views []HookView
	for _, h := range s.hooks {
		muted := hasSavedHook(prof.MutedHooks, config.EmbedHook{HookCode: h.HookCode, Ctx: h.Ctx, Ctx2: h.Ctx2})
		if muted {
			s.muted[h.Key] = true
			delete(s.embedding, h.Key)
			views = append(views, s.buildHookViewLocked(h, nowMs))
			continue
		}
		if hasSavedHook(prof.EmbedHooks, config.EmbedHook{HookCode: h.HookCode, Ctx: h.Ctx, Ctx2: h.Ctx2}) && !s.embedding[h.Key] {
			s.embedding[h.Key] = true
			enableList = append(enableList, h)
			views = append(views, s.buildHookViewLocked(h, nowMs))
		}
	}
	s.mu.Unlock()

	if s.host != nil {
		for _, h := range enableList {
			s.host.UseEmbed(h.TP, true)
		}
	}
	for _, v := range views {
		s.post(EventHookNew, v)
	}
}

func hasSavedHook(list []config.EmbedHook, target config.EmbedHook) bool {
	for _, item := range list {
		if item.HookCode == target.HookCode && item.Ctx == target.Ctx && item.Ctx2 == target.Ctx2 {
			return true
		}
	}
	return false
}

func hasRule(list []config.Rule, target config.Rule) bool {
	for _, r := range list {
		if r.Regex == target.Regex && r.Pattern == target.Pattern && r.Replace == target.Replace {
			return true
		}
	}
	return false
}

func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "game-profile"
	}
	return out
}
