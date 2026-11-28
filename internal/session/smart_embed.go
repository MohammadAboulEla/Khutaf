//go:build windows

package session

import (
	"errors"
	"sort"
	"strings"
	"unicode"

	"khutaf/internal/hook"
)

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
