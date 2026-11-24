//go:build windows

package main

import (
	"slices"
	"testing"
)

func TestListFontsVerifiesArabicGlyphs(t *testing.T) {
	fonts := (&App{}).ListFonts()
	t.Logf("verified Arabic fonts (%d): %v", len(fonts), fonts)

	for _, required := range []string{"Tahoma", "Arial", "Calibri"} {
		if !slices.Contains(fonts, required) {
			t.Fatalf("expected %q in verified Arabic fonts, got %v", required, fonts)
		}
	}
	for _, latin := range []string{"Impact", "Comic Sans MS"} {
		if slices.Contains(fonts, latin) {
			t.Fatalf("unexpected Latin-only font %q in %v", latin, fonts)
		}
	}
}

func TestListAllFontsIncludesMoreThanFiltered(t *testing.T) {
	app := &App{}
	filtered := app.ListFonts()
	all := app.ListAllFonts()
	t.Logf("filtered=%d all=%d", len(filtered), len(all))
	if len(all) < len(filtered) {
		t.Fatalf("expected all fonts (%d) >= filtered fonts (%d)", len(all), len(filtered))
	}
}
