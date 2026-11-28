//go:build windows

package session

import (
	"encoding/json"
	"strings"
	"testing"

	"khutaf/internal/config"
	"khutaf/internal/hook"
)

func TestParseNativeHookMeta(t *testing.T) {
	hid, ctx, ctx2, path, ok := parseNativeHookMeta("[meta|6|4a21c0|0] Canvas/DialoguePanel/BodyText")
	if !ok {
		t.Fatal("expected valid meta line")
	}
	if hid != 6 || ctx != 0x4a21c0 || ctx2 != 0 || path != "Canvas/DialoguePanel/BodyText" {
		t.Fatalf("unexpected parsed meta: hid=%d ctx=%x ctx2=%x path=%q", hid, ctx, ctx2, path)
	}
	if _, _, _, _, ok := parseNativeHookMeta("regular log line"); ok {
		t.Fatal("expected false for non-meta line")
	}
}

func TestClassifyHookCategories(t *testing.T) {
	now := int64(1_000_000)

	// Dialogue by text + path
	dlgInfo := hook.HookInfo{Key: "1:1:a:0", HookCode: "UNITY:TextMeshProUGUI.set_text", HookName: "TMP", Embeddable: true}
	dlgIntel := &hookIntel{}
	dlgIntel.setObjectPath(dlgInfo, "Canvas/DialoguePanel/BodyText", now)
	dlgIntel.recordText(dlgInfo, "Welcome to the citadel, traveler. We have waited long for your return!", now, false)
	if dlgIntel.category != HookCatDialogue || dlgIntel.dialogueScore < 70 {
		t.Fatalf("expected dialogue high score, got cat=%s score=%d", dlgIntel.category, dlgIntel.dialogueScore)
	}

	// Speaker co-changing with dialogue
	spkInfo := hook.HookInfo{Key: "1:1:b:0", HookCode: "UNITY:TextMeshProUGUI.set_text", HookName: "TMP", Embeddable: true}
	spkIntel := &hookIntel{}
	spkIntel.recordText(spkInfo, "Elena", now+50, true)
	spkIntel.recordText(spkInfo, "Captain Vance", now+1500, true)
	if spkIntel.category != HookCatSpeaker {
		t.Fatalf("expected speaker category, got %s (score=%d)", spkIntel.category, spkIntel.dialogueScore)
	}

	// Static UI menu
	uiInfo := hook.HookInfo{Key: "1:12:0:0", HookCode: "UNITY:TMP.StaticUI", HookName: "StaticUI", Embeddable: true}
	uiIntel := &hookIntel{}
	uiIntel.recordText(uiInfo, "Settings", now, false)
	if uiIntel.category != HookCatUI {
		t.Fatalf("expected ui category for StaticUI, got %s", uiIntel.category)
	}

	// Noise: high-frequency FPS/counter
	noiseInfo := hook.HookInfo{Key: "1:1:c:0", HookCode: "UNITY:TMP_Text.set_text", HookName: "TMP", Embeddable: true}
	noiseIntel := &hookIntel{}
	for i := range 8 {
		noiseIntel.recordText(noiseInfo, "60 FPS", now+int64(i*100), false)
	}
	if noiseIntel.category != HookCatNoise {
		t.Fatalf("expected noise category for rapid FPS counter, got %s (score=%d)", noiseIntel.category, noiseIntel.dialogueScore)
	}
}

func TestPerHookHistoryRingBufferAndDeduplication(t *testing.T) {
	info := hook.HookInfo{Key: "1:1:d:0", HookCode: "UNITY:TMP_Text.set_text", Embeddable: true}
	hi := &hookIntel{}
	for i := range 20 {
		hi.recordText(info, "Dialogue line "+string(rune('A'+i)), int64(1000+i*100), false)
	}
	if len(hi.history) != maxHookHistorySamples {
		t.Fatalf("expected history capped at %d, got %d", maxHookHistorySamples, len(hi.history))
	}
	// Re-recording an existing line moves it to front without duplicating.
	hi.recordText(info, "Dialogue line S", 5000, false)
	if hi.history[0].Text != "Dialogue line S" || len(hi.history) != maxHookHistorySamples {
		t.Fatalf("unexpected front after dedup: %+v", hi.history[0])
	}
}

func TestRollingDialogueContextAndSpeaker(t *testing.T) {
	s := New(config.NewMemoryStore(), "", nil)
	defer s.Close()

	dlg := hook.HookInfo{Key: "1:1:a:0", Addr: 1, Ctx: 0xa, HookCode: "UNITY:TMP_Text.set_text", Embeddable: true}
	spk := hook.HookInfo{Key: "1:1:b:0", Addr: 1, Ctx: 0xb, HookCode: "UNITY:TMP_Text.set_text", Embeddable: true}
	s.OnNewHook(dlg)
	s.OnNewHook(spk)
	s.applyNativeHookMeta(1, 0xa, 0, "DialogueCanvas/SpeechBox/BodyText")
	s.applyNativeHookMeta(1, 0xb, 0, "DialogueCanvas/SpeechBox/SpeakerName")

	s.OnOutput(spk, "Aria")
	s.OnOutput(dlg, "Do you really think we can cross the valley before dusk?")
	s.recordDialogueTurn(dlg.Key, "Do you really think we can cross the valley before dusk?", "هل تظن حقاً أننا نستطيع عبور الوادي قبل الغسق؟")

	prompt := s.buildDialogueContextPrompt(dlg.Key)
	if !strings.Contains(prompt, "[Aria]") || !strings.Contains(prompt, "Current speaker: Aria") || !strings.Contains(prompt, "عبور الوادي") {
		t.Fatalf("expected rolling context with speaker and previous translation, got:\n%s", prompt)
	}
}

func TestExportImportGameProfileAndGlossaryCapture(t *testing.T) {
	store := config.NewMemoryStore()
	s := New(store, "", nil)
	defer s.Close()

	gameA := `C:\Games\RPG\GameA.exe`
	gameB := `D:\Other\GameA.exe`
	if err := store.UpdateGame(gameA, func(g *config.GameProfile) {
		g.Title = "Epic RPG"
		g.EmbedHooks = []config.EmbedHook{{HookCode: "UNITY:TMP_Text.set_text", Ctx: 0x1234, Sample: "Hello"}}
		g.MutedHooks = []config.EmbedHook{{HookCode: "UNITY:TMP_Text.set_text", Ctx: 0x9999}}
		g.Pipeline.Glossary = []config.GlossaryEntry{{Enabled: true, Source: "Valoria", Target: "فالوريا"}}
		g.TranslatorOverride = &config.TranslatorSettings{
			Active: "llm",
			LLM:    config.LLMSettings{APIKey: "sk-super-secret"},
		}
	}); err != nil {
		t.Fatal(err)
	}

	raw, filename, err := s.ExportGameProfileData(gameA, "0.2.4")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if !strings.HasSuffix(filename, ".khutaf.json") {
		t.Fatalf("unexpected filename %q", filename)
	}
	if strings.Contains(string(raw), "sk-super-secret") || strings.Contains(string(raw), `C:\Games`) {
		t.Fatalf("exported profile leaked secret or local path: %s", string(raw))
	}

	// Verify JSON validity
	var parsed ShareableGameProfile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}

	res, err := s.ImportGameProfileData(gameB, raw)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if res.HooksAdded != 1 || res.MutedAdded != 1 || res.GlossaryAdded != 1 {
		t.Fatalf("unexpected import result: %+v", res)
	}

	// Re-importing same file should not duplicate entries.
	res2, err := s.ImportGameProfileData(gameB, raw)
	if err != nil {
		t.Fatalf("second import failed: %v", err)
	}
	if res2.HooksAdded != 0 || res2.MutedAdded != 0 || res2.GlossaryAdded != 0 {
		t.Fatalf("expected idempotent re-import, got %+v", res2)
	}

	// Quick glossary capture into global glossary (when no game attached).
	if err := s.AddGlossaryTerm("Aether", "الأثير", false, false); err != nil {
		t.Fatalf("AddGlossaryTerm failed: %v", err)
	}
	found := false
	for _, g := range s.Config().Pipeline.Glossary {
		if g.Source == "Aether" && g.Target == "الأثير" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected Aether term in global glossary")
	}
}
