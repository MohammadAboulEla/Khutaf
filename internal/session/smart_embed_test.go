//go:build windows

package session

import (
	"reflect"
	"testing"

	"khutaf/internal/config"
	"khutaf/internal/hook"
)

func TestSelectSmartEmbedHooksTiers(t *testing.T) {
	// Tier 1: prefers embeddable Unity text hooks with meaningful captured text.
	hooks := []hook.HookInfo{
		{Key: "1:a", HookName: "Unity_TextMeshProUGUI", HookCode: "UNITY:TMPro.TMP_Text.set_text", Embeddable: true},
		{Key: "1:b", HookName: "Unity_UI_Text", HookCode: "UNITY:UnityEngine.UI.Text.set_text", Embeddable: true},
		{Key: "1:c", HookName: "GenericWin32", HookCode: "HS10@USER32", Embeddable: true},
		{Key: "1:d", HookName: "Unity_ReadOnly", HookCode: "UNITY:TMP_Text", Embeddable: false},
	}
	lastOut := map[string]string{
		"1:a": "Welcome to the guild, adventurer!",
		"1:b": "123", // numbers only, not meaningful dialogue yet
		"1:c": "Window Title",
		"1:d": "Should never be selected",
	}

	got := selectSmartEmbedHooks(hooks, lastOut)
	want := []string{"1:a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tier 1 got %v, want %v", got, want)
	}

	// Tier 2: when no text captured yet, selects all embeddable Unity text hooks.
	got = selectSmartEmbedHooks(hooks, map[string]string{})
	want = []string{"1:a", "1:b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tier 2 got %v, want %v", got, want)
	}

	// Tier 3: when only non-Unity embeddable hooks exist, falls back to them.
	genericOnly := []hook.HookInfo{
		{Key: "2:a", HookName: "CustomHookA", HookCode: "HS0@foo", Embeddable: true},
		{Key: "2:b", HookName: "CustomHookB", HookCode: "HS1@bar", Embeddable: false},
	}
	got = selectSmartEmbedHooks(genericOnly, map[string]string{})
	want = []string{"2:a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tier 3 got %v, want %v", got, want)
	}
}

func TestSmartAutoEmbedRequiresAttachedGame(t *testing.T) {
	s := New(config.NewMemoryStore(), "", nil)
	defer s.Close()

	if _, err := s.SmartAutoEmbed(); err == nil {
		t.Fatal("expected error when no game is attached")
	}
}
