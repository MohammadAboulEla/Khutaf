package arabic

import "testing"

func runes(rs ...rune) string { return string(rs) }

func TestShapeContextualForms(t *testing.T) {
	// مرحبا -> initial meem, final reh, initial hah, medial beh, final alef
	got := Shape("مرحبا")
	want := runes(0xFEE3, 0xFEAE, 0xFEA3, 0xFE92, 0xFE8E)
	if got != want {
		t.Fatalf("Shape = %U, want %U", []rune(got), []rune(want))
	}
}

func TestShapeLamAlef(t *testing.T) {
	if got := Shape("لا"); got != runes(0xFEFB) {
		t.Fatalf("isolated lam-alef = %U", []rune(got))
	}
	// بلا -> initial beh + final lam-alef
	if got := Shape("بلا"); got != runes(0xFE91, 0xFEFC) {
		t.Fatalf("final lam-alef = %U", []rune(got))
	}
	// لأ with hamza
	if got := Shape("لأ"); got != runes(0xFEF7) {
		t.Fatalf("lam-alef hamza = %U", []rune(got))
	}
}

func TestShapeKeepsTashkeelAndJoining(t *testing.T) {
	// بَب : fatha must not break the join between the two behs
	got := Shape("بَب")
	want := runes(0xFE91, 0x064E, 0xFE90)
	if got != want {
		t.Fatalf("Shape = %U, want %U", []rune(got), []rune(want))
	}
}

func TestVisualOrderPreservesLTRRuns(t *testing.T) {
	got := VisualOrder(Shape("مرحبا Ahmed 123"))
	want := "123 Ahmed " + runes(0xFE8E, 0xFE92, 0xFEA3, 0xFEAE, 0xFEE3)
	if got != want {
		t.Fatalf("VisualOrder = %q, want %q", got, want)
	}
}

func TestVisualOrderMirrorsBrackets(t *testing.T) {
	got := VisualOrder(Shape("(بب)"))
	want := "(" + runes(0xFE90, 0xFE91) + ")"
	if got != want {
		t.Fatalf("VisualOrder = %q, want %q", got, want)
	}
}

func TestVisualOrderKeepsDiacriticAfterBase(t *testing.T) {
	got := VisualOrder(Shape("بَب"))
	want := runes(0xFE90, 0xFE91, 0x064E)
	if got != want {
		t.Fatalf("VisualOrder = %U, want %U", []rune(got), []rune(want))
	}
}

func TestVisualOrderKeepsTagsWellFormed(t *testing.T) {
	got := VisualOrder(Shape("<color=red>بب</color> {name}"))
	want := "{name} <color=red>" + runes(0xFE90, 0xFE91) + "</color>"
	if got != want {
		t.Fatalf("VisualOrder = %q, want %q", got, want)
	}
}

func TestWrapIsLogicalBeforeReversal(t *testing.T) {
	text := "واحد اثنان ثلاثة"
	wrapped := Wrap(text, 10)
	if wrapped != "واحد اثنان\nثلاثة" {
		t.Fatalf("Wrap = %q", wrapped)
	}
	out := Process(text, Options{MaxLineLength: 10})
	want := VisualOrder(Shape("واحد اثنان")) + "\n" + VisualOrder(Shape("ثلاثة"))
	if out != want {
		t.Fatalf("Process = %q, want %q", out, want)
	}
}

func TestProcessOriTransPrefix(t *testing.T) {
	out := Process("بب", Options{DisplayMode: DisplayOriTrans})
	if out[0] != '\n' {
		t.Fatalf("expected leading newline, got %q", out)
	}
}

func TestProcessLeavesNonArabic(t *testing.T) {
	if out := Process("Hello world", Options{}); out != "Hello world" {
		t.Fatalf("Process = %q", out)
	}
}
