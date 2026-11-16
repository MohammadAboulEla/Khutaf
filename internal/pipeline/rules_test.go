package pipeline

import (
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
	if !PlaceholdersPreserved("Hi [name], {0} gold", "مرحبا [name]، {0} ذهب") {
		t.Fatal("expected preserved")
	}
	if PlaceholdersPreserved("Hi [name]", "مرحبا") {
		t.Fatal("expected missing placeholder to fail")
	}
}
