package arabic

import (
	"regexp"
	"strings"
)

// ltrRun matches runs that must keep their internal left-to-right order:
// rich-text tags, [vars], {vars}, Latin words and numbers (ASCII or Arabic-Indic).
var ltrRun = regexp.MustCompile(`<[^>]+>|\[[^\]]+\]|\{[^\}]+\}|[A-Za-z0-9\x{0660}-\x{0669}]+(?:[.,:/' -]+[A-Za-z0-9\x{0660}-\x{0669}]+)*`)

var mirror = map[rune]rune{
	'(': ')', ')': '(',
	'[': ']', ']': '[',
	'{': '}', '}': '{',
	'<': '>', '>': '<',
	'«': '»', '»': '«',
}

// VisualOrder converts one logical line (already shaped) into left-to-right
// visual order: RTL characters are reversed and mirrored, LTR runs stay intact,
// and paired rich-text tags are swapped so they remain well-formed after reversal.
func VisualOrder(line string) string {
	if !hasRTL(line) {
		return line
	}
	tokens := tokenize(line)
	swapPairedTags(tokens)
	var b strings.Builder
	b.Grow(len(line))
	for i := len(tokens) - 1; i >= 0; i-- {
		b.WriteString(tokens[i])
	}
	return b.String()
}

// tokenize splits a line into LTR runs (kept whole) and RTL clusters
// (one base character plus its trailing diacritics).
func tokenize(line string) []string {
	tokens := make([]string, 0, len(line))
	appendRTL := func(segment string) {
		for _, r := range segment {
			if m, ok := mirror[r]; ok {
				r = m
			}
			if IsDiacritic(r) && len(tokens) > 0 {
				// Keep base-then-mark order so the mark still combines with its base.
				tokens[len(tokens)-1] += string(r)
				continue
			}
			tokens = append(tokens, string(r))
		}
	}
	last := 0
	for _, loc := range ltrRun.FindAllStringIndex(line, -1) {
		appendRTL(line[last:loc[0]])
		tokens = append(tokens, line[loc[0]:loc[1]])
		last = loc[1]
	}
	appendRTL(line[last:])
	return tokens
}

// swapPairedTags exchanges the text of matching <tag>...</tag> tokens so that
// after the token list is reversed the opening tag precedes the closing tag again.
func swapPairedTags(tokens []string) {
	type open struct {
		name  string
		index int
	}
	var stack []open
	for i, tok := range tokens {
		if len(tok) < 3 || tok[0] != '<' || tok[len(tok)-1] != '>' {
			continue
		}
		if tok[1] == '/' {
			name := tagName(tok[2 : len(tok)-1])
			for s := len(stack) - 1; s >= 0; s-- {
				if stack[s].name == name || name == "" {
					o := stack[s].index
					tokens[o], tokens[i] = tokens[i], tokens[o]
					stack = stack[:s]
					break
				}
			}
			continue
		}
		if tok[len(tok)-2] == '/' {
			continue // self-closing
		}
		stack = append(stack, open{name: tagName(tok[1 : len(tok)-1]), index: i})
	}
}

// hasRTL is HasArabic extended with Presentation Forms-A/B (i.e. already-shaped text).
func hasRTL(s string) bool {
	for _, r := range s {
		if (r >= 0xFB50 && r <= 0xFDFF) || (r >= 0xFE70 && r <= 0xFEFF) {
			return true
		}
	}
	return HasArabic(s)
}

func tagName(body string) string {
	end := strings.IndexAny(body, "= ")
	if end >= 0 {
		body = body[:end]
	}
	return strings.ToLower(strings.TrimSpace(body))
}
