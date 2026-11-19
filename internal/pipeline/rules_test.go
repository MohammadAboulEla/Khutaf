package pipeline

import (
	"strings"
	"testing"

	"khutaf/internal/config"
)

func TestRuleSetApply(t *testing.T) {
	rs, err := Compile([]config.Rule{
		{Enabled: true, Regex: true, Pattern: `\s+`, Replace: " "},
		{Enabled: true, Pattern: "foo", Replace: "bar"},
		{Enabled: false, Pattern: "bar", Replace: "baz"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := rs.Apply("foo   x\n y"); got != "bar x y" {
		t.Fatalf("Apply = %q", got)
	}
}

func TestCompileRejectsBadRegex(t *testing.T) {
	if _, err := Compile([]config.Rule{{Enabled: true, Regex: true, Pattern: "("}}); err == nil {
		t.Fatal("expected error")
	}
}

func TestPlaceholdersPreserved(t *testing.T) {
	cases := []struct {
		name   string
		source string
		trans  string
		want   bool
	}{
		{"kept_all", "Hi [name], {0} gold", "مرحبا [name]، {0} ذهب", true},
		{"missing_lower", "Hi [name]", "مرحبا", false},
		{"camelcase_kept", "Hello [PlayerName] ([HeroHP])", "مرحبا [PlayerName] ([HeroHP])", true},
		{"camelcase_dropped", "Hello [PlayerName]", "مرحبا", false},
		{"narrative_bracket_translated", "[Sigh] I know", "[تنهد] أعرف", true},
		{"whispering_translated", "[Whispering] Run!", "[همس] اهرب!", true},
		{"bbcode_kept", "[b]Bold[/b]", "[b]عريض[/b]", true},
		{"duplicate_multiset_mismatch", "{0} hit {0}", "{0} ضرب", false},
		{"duplicate_multiset_match", "{0} hit {0}", "{0} ضرب {0}", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlaceholdersPreserved(tc.source, tc.trans); got != tc.want {
				t.Fatalf("PlaceholdersPreserved(%q, %q) = %v, want %v", tc.source, tc.trans, got, tc.want)
			}
		})
	}
}

func TestGlossaryApplyAndPromptContext(t *testing.T) {
	global := []config.GlossaryEntry{
		{Enabled: true, Source: "Key", Target: "مفتاح"},
		{Enabled: true, Source: "Wonderia", Target: "وونديريا"},
		{Enabled: false, Source: "Disabled", Target: "معطل"},
	}
	game := []config.GlossaryEntry{
		{Enabled: true, Source: "Silver Key", Target: "المفتاح الفضي"},
		{Enabled: true, Source: "wonderia", Target: "عالم وونديريا", CaseSensitive: true},
	}

	g := CompileGlossary(global, game)

	// Word-boundary and longest-first priority: "Silver Key" before "Key", and "Monkey" untouched.
	got := g.Apply("Find the silver key in Monkey Island, not wonderia or Wonderia.")
	want := "Find the المفتاح الفضي in Monkey Island, not عالم وونديريا or Wonderia."
	if got != want {
		t.Fatalf("Glossary.Apply = %q, want %q", got, want)
	}

	prompt := g.PromptContext("Take the Silver Key to wonderia.")
	if !strings.Contains(prompt, "Silver Key -> المفتاح الفضي") {
		t.Fatalf("PromptContext missing Silver Key: %q", prompt)
	}
	if !strings.Contains(prompt, "wonderia -> عالم وونديريا") {
		t.Fatalf("PromptContext missing game override: %q", prompt)
	}
	if strings.Contains(prompt, "Disabled") {
		t.Fatalf("PromptContext included disabled entry: %q", prompt)
	}
	if got := g.PromptContext("Monkey"); got != "" {
		t.Fatalf("PromptContext matched partial word unexpectedly: %q", got)
	}
}
