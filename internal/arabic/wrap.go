package arabic

import (
	"strings"
)

// DisplayMode mirrors khutaf-hook64's display mode enum.
type DisplayMode int

const (
	DisplayTrans    DisplayMode = iota // translation only
	DisplayOriTrans                    // original + translation
	DisplayTransOri                    // translation + original
	DisplayOri                         // original only
)

// DefaultLineLength is the default maximum visible characters per wrapped line.
const DefaultLineLength = 40

// Options controls Process.
type Options struct {
	// MaxLineLength is the wrap width in visible characters; <= 0 uses DefaultLineLength.
	MaxLineLength int
	// ForceWrap wraps non-Arabic text too (Arabic text is always wrapped).
	ForceWrap   bool
	DisplayMode DisplayMode
}

// Process prepares a translated string for in-game embedding:
// logical word-wrap first, then contextual shaping and visual reordering per line.
func Process(text string, opts Options) string {
	if text == "" {
		return text
	}
	hasArabic := HasArabic(text)
	if hasArabic || opts.ForceWrap {
		text = Wrap(text, opts.MaxLineLength)
	}
	if !hasArabic {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = VisualOrder(Shape(line))
	}
	return strings.Join(lines, "\n")
}

// Wrap splits each line on spaces into lines of at most maxLen visible characters.
// Spaces inside <...> rich-text tags are never split, and words longer than maxLen
// are kept whole on their own line.
func Wrap(text string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = DefaultLineLength
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		words := splitWords(line)
		var para []string
		current := words[0]
		currentLen := visibleLen(words[0])
		for _, w := range words[1:] {
			wl := visibleLen(w)
			if currentLen+1+wl <= maxLen {
				current += " " + w
				currentLen += 1 + wl
				continue
			}
			para = append(para, current)
			current, currentLen = w, wl
		}
		para = append(para, current)
		if len(para) > 1 {
			balanceRichTextTags(para)
		}
		out = append(out, para...)
	}
	return strings.Join(out, "\n")
}

type richTag struct {
	name    string
	openTag string
}

var voidRichTags = map[string]bool{
	"br":     true,
	"sprite": true,
	"space":  true,
	"page":   true,
}

// balanceRichTextTags closes any open rich-text tags at the end of a wrapped line
// and reopens them at the beginning of the next line so BiDi reordering per line
// always sees matched tag pairs.
func balanceRichTextTags(lines []string) {
	var stack []richTag
	for idx, line := range lines {
		var prefix strings.Builder
		for _, t := range stack {
			prefix.WriteString(t.openTag)
		}

		for i := 0; i < len(line); i++ {
			if line[i] != '<' {
				continue
			}
			end := strings.IndexByte(line[i:], '>')
			if end <= 1 {
				continue
			}
			full := line[i : i+end+1]
			inner := line[i+1 : i+end]
			i += end

			if inner[0] == '/' {
				name := tagName(inner[1:])
				for s := len(stack) - 1; s >= 0; s-- {
					if stack[s].name == name || name == "" {
						stack = stack[:s]
						break
					}
				}
				continue
			}
			if inner[len(inner)-1] == '/' {
				continue
			}
			name := tagName(inner)
			if name == "" || voidRichTags[name] {
				continue
			}
			stack = append(stack, richTag{name: name, openTag: full})
		}

		var suffix strings.Builder
		if idx < len(lines)-1 {
			for s := len(stack) - 1; s >= 0; s-- {
				suffix.WriteString("</")
				suffix.WriteString(stack[s].name)
				suffix.WriteByte('>')
			}
		}

		if prefix.Len() > 0 || suffix.Len() > 0 {
			lines[idx] = prefix.String() + line + suffix.String()
		}
	}
}

// splitWords splits line on spaces outside <...> tags so multi-attribute tags stay intact.
func splitWords(line string) []string {
	var words []string
	start := 0
	inTag := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '<':
			if strings.IndexByte(line[i:], '>') > 0 {
				inTag = true
			}
		case '>':
			inTag = false
		case ' ':
			if !inTag {
				words = append(words, line[start:i])
				start = i + 1
			}
		}
	}
	words = append(words, line[start:])
	return words
}

// visibleLen counts characters excluding diacritics and rich-text tags.
func visibleLen(s string) int {
	n := 0
	inTag := false
	for i, r := range s {
		switch {
		case r == '<':
			if strings.IndexByte(s[i:], '>') > 0 {
				inTag = true
			} else {
				n++
			}
		case r == '>' && inTag:
			inTag = false
		case inTag, IsDiacritic(r):
		default:
			n++
		}
	}
	return n
}
