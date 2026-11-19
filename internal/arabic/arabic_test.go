package arabic

import (
	"strings"
	"testing"
)

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
	want := "Ahmed 123 " + runes(0xFE8E, 0xFE92, 0xFEA3, 0xFEAE, 0xFEE3)
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

func TestWrapPreservesSpaceInsideRichTag(t *testing.T) {
	in := `إذا كنت تبحث عن <font="HP_Normal_fnt" material=en_he:dialogues_accent_dynamic><color=#037AAB>الخشب</color></font> والحجر`
	wrapped := Wrap(in, 25)
	if strings.Contains(wrapped, "<font=\"HP_Normal_fnt\"\n") {
		t.Fatalf("Wrap split inside <font ...> tag:\n%s", wrapped)
	}
	out := Process(in, Options{MaxLineLength: 25})
	if !strings.Contains(out, `<font="HP_Normal_fnt" material=en_he:dialogues_accent_dynamic>`) {
		t.Fatalf("Process mangled multi-attr <font> tag:\n%s", out)
	}
}

func TestProcessOriTransNoExtraNewline(t *testing.T) {
	out := Process("بب", Options{DisplayMode: DisplayOriTrans})
	if strings.HasPrefix(out, "\n") || strings.HasSuffix(out, "\n") {
		t.Fatalf("unexpected leading/trailing newline: %q", out)
	}
}

func TestProcessLeavesNonArabic(t *testing.T) {
	if out := Process("Hello world", Options{}); out != "Hello world" {
		t.Fatalf("Process = %q", out)
	}
}

func TestVisualOrderMultiWordLatinPhrase(t *testing.T) {
	got := VisualOrder(Shape("اضغط على Start New Game للبدء"))
	want := VisualOrder(Shape("للبدء")) + " Start New Game " + VisualOrder(Shape("اضغط على"))
	if got != want {
		t.Fatalf("VisualOrder = %q, want %q", got, want)
	}
}

func TestWrapAndProcessBalancesMultilineRichTags(t *testing.T) {
	in := "<color=#FF0000>واحد اثنان ثلاثة</color>"
	wrapped := Wrap(in, 10)
	wantWrapped := "<color=#FF0000>واحد اثنان</color>\n<color=#FF0000>ثلاثة</color>"
	if wrapped != wantWrapped {
		t.Fatalf("Wrap = %q, want %q", wrapped, wantWrapped)
	}

	out := Process(in, Options{MaxLineLength: 10})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	for i, line := range lines {
		openIdx := strings.Index(line, "<color=#FF0000>")
		closeIdx := strings.Index(line, "</color>")
		if openIdx < 0 || closeIdx < 0 || openIdx > closeIdx {
			t.Fatalf("line %d has unbalanced rich-text tag order: %q", i, line)
		}
	}
}

func TestWrapAndVisualOrderUnclosedLessThan(t *testing.T) {
	in := "الصحة < 50 نقطة"
	wrapped := Wrap(in, 12)
	wantWrapped := "الصحة < 50\nنقطة"
	if wrapped != wantWrapped {
		t.Fatalf("Wrap = %q, want %q", wrapped, wantWrapped)
	}

	vis := VisualOrder(Shape("الصحة < 50"))
	wantVis := "50 > " + VisualOrder(Shape("الصحة"))
	if vis != wantVis {
		t.Fatalf("VisualOrder = %q, want %q", vis, wantVis)
	}
}

func TestShapeExtendedArabicLetters(t *testing.T) {
	if got := Shape("ڤ"); got != runes(0xFB6A) {
		t.Fatalf("isolated veh = %U, want U+FB6A", []rune(got))
	}
	if got := Shape("بڤب"); got != runes(0xFE91, 0xFB6D, 0xFE90) {
		t.Fatalf("medial veh = %U, want [U+FE91 U+FB6D U+FE90]", []rune(got))
	}
	if got := Shape("ٱ"); got != runes(0xFB50) {
		t.Fatalf("isolated alef wasla = %U, want U+FB50", []rune(got))
	}
	if got := Shape("بٱ"); got != runes(0xFE91, 0xFB51) {
		t.Fatalf("final alef wasla = %U, want [U+FE91 U+FB51]", []rune(got))
	}
}
