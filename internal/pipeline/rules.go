// Package pipeline implements text processing around translation:
// pre-translation rules, post-translation correction and placeholder safety checks.
package pipeline

import (
	"fmt"
	"regexp"
	"strings"

	"khutaf/internal/config"
)

type compiledRule struct {
	re      *regexp.Regexp
	literal string
	replace string
}

// RuleSet is a compiled, ordered list of replacement rules.
type RuleSet struct{ rules []compiledRule }

// Compile builds a RuleSet, skipping disabled or empty rules.
func Compile(rules []config.Rule) (*RuleSet, error) {
	rs := &RuleSet{}
	for i, r := range rules {
		if !r.Enabled || r.Pattern == "" {
			continue
		}
		cr := compiledRule{literal: r.Pattern, replace: r.Replace}
		if r.Regex {
			re, err := regexp.Compile(r.Pattern)
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", i+1, err)
			}
			cr.re = re
		}
		rs.rules = append(rs.rules, cr)
	}
	return rs, nil
}

// Apply runs all rules in order.
func (rs *RuleSet) Apply(s string) string {
	if rs == nil {
		return s
	}
	for _, r := range rs.rules {
		if r.re != nil {
			s = r.re.ReplaceAllString(s, r.replace)
		} else {
			s = strings.ReplaceAll(s, r.literal, r.replace)
		}
	}
	return s
}

var protectedMarks = []*regexp.Regexp{
	regexp.MustCompile(`\[(?:\d+|[A-Z0-9_]+|[a-z][a-z0-9_]*|[A-Za-z0-9_.:=-]*[:=_-][^\]]*)\]`),
	regexp.MustCompile(`\{[^\}]+\}`),
}

// PlaceholdersPreserved reports whether trans contains the exact same multiset of
// technical [vars] and {vars} as the source. Injecting a translation that drops
// them can crash games that format these strings.
func PlaceholdersPreserved(source, trans string) bool {
	for _, re := range protectedMarks {
		if !sameMultiset(re.FindAllString(source, -1), re.FindAllString(trans, -1)) {
			return false
		}
	}
	return true
}

func sameMultiset(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}
