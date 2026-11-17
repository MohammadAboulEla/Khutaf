package pipeline

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"khutaf/internal/config"
)

type compiledTerm struct {
	source string
	target string
	re     *regexp.Regexp
}

// Glossary holds compiled proper-noun/term replacements ordered longest-first.
type Glossary struct {
	terms []compiledTerm
}

type mergedGlossaryEntry struct {
	entry config.GlossaryEntry
	order int
}

// CompileGlossary merges glossary scopes (later scopes override earlier ones by
// case-insensitive source term) and compiles word-boundary matchers.
func CompileGlossary(scopes ...[]config.GlossaryEntry) *Glossary {
	byKey := make(map[string]mergedGlossaryEntry)
	seq := 0
	for _, scope := range scopes {
		for _, e := range scope {
			if !e.Enabled {
				continue
			}
			src := strings.TrimSpace(e.Source)
			tgt := strings.TrimSpace(e.Target)
			if src == "" || tgt == "" {
				continue
			}
			key := strings.ToLower(src)
			e.Source = src
			e.Target = tgt
			if prev, exists := byKey[key]; exists {
				byKey[key] = mergedGlossaryEntry{entry: e, order: prev.order}
			} else {
				byKey[key] = mergedGlossaryEntry{entry: e, order: seq}
				seq++
			}
		}
	}
	if len(byKey) == 0 {
		return &Glossary{}
	}

	merged := make([]mergedGlossaryEntry, 0, len(byKey))
	for _, item := range byKey {
		merged = append(merged, item)
	}
	sort.Slice(merged, func(i, j int) bool {
		li := utf8.RuneCountInString(merged[i].entry.Source)
		lj := utf8.RuneCountInString(merged[j].entry.Source)
		if li != lj {
			return li > lj
		}
		return merged[i].order < merged[j].order
	})

	terms := make([]compiledTerm, 0, len(merged))
	for _, item := range merged {
		re := compileTermRegex(item.entry.Source, item.entry.CaseSensitive)
		if re == nil {
			continue
		}
		terms = append(terms, compiledTerm{
			source: item.entry.Source,
			target: item.entry.Target,
			re:     re,
		})
	}
	return &Glossary{terms: terms}
}

func compileTermRegex(source string, caseSensitive bool) *regexp.Regexp {
	runes := []rune(source)
	pat := regexp.QuoteMeta(source)
	if len(runes) > 0 && isASCIIWord(runes[0]) {
		pat = `\b` + pat
	}
	if len(runes) > 0 && isASCIIWord(runes[len(runes)-1]) {
		pat += `\b`
	}
	if !caseSensitive {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	return re
}

func isASCIIWord(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// Apply replaces matching source terms with their glossary targets.
func (g *Glossary) Apply(s string) string {
	if g == nil || len(g.terms) == 0 || s == "" {
		return s
	}
	for _, t := range g.terms {
		s = t.re.ReplaceAllLiteralString(s, t.target)
	}
	return s
}

// PromptContext formats matching glossary terms for LLM system prompt injection.
func (g *Glossary) PromptContext(s string) string {
	if g == nil || len(g.terms) == 0 || s == "" {
		return ""
	}
	var lines []string
	for _, t := range g.terms {
		if t.re.MatchString(s) {
			lines = append(lines, t.source+" -> "+t.target)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Glossary (use these exact translations for proper nouns/terms):\n- " + strings.Join(lines, "\n- ")
}
