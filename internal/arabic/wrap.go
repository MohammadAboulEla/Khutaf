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
	text = strings.Join(lines, "\n")
	if opts.DisplayMode == DisplayOriTrans && !strings.HasPrefix(text, "\n") {
		// Start Arabic on a fresh line below the original text.
		text = "\n" + text
	} else if opts.DisplayMode == DisplayTransOri && !strings.HasSuffix(text, "\n") {
		// Put the original text on a fresh line below the Arabic translation.
		text += "\n"
	}
	return text
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
		current := words[0]
		currentLen := visibleLen(words[0])
		for _, w := range words[1:] {
			wl := visibleLen(w)
			if currentLen+1+wl <= maxLen {
				current += " " + w
				currentLen += 1 + wl
				continue
			}
			out = append(out, current)
			current, currentLen = w, wl
		}
		out = append(out, current)
	}
	return strings.Join(out, "\n")
}

// splitWords splits line on spaces outside <...> tags so multi-attribute tags stay intact.
func splitWords(line string) []string {
	var words []string
	start := 0
	inTag := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '<':
			inTag = true
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
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
		case inTag, IsDiacritic(r):
		default:
			n++
		}
	}
	return n
}
