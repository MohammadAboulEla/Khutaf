package translator

import (
	"context"
	"strings"
	"testing"

	"khutaf/internal/config"
)

func TestWithDialogueContextAndOfflineTemplates(t *testing.T) {
	ctx := WithDialogueContext(context.Background(), "Recent story dialogue:\n[Aria] Ready? => مستعد؟\nCurrent speaker: Aria")
	got := DialogueContextFromContext(ctx)
	if !strings.Contains(got, "Current speaker: Aria") {
		t.Fatalf("unexpected context value: %q", got)
	}

	for _, tpl := range []string{"hymt2", "translate_gemma", "nllb_instruct", "generic_local"} {
		o := NewOfflineLLM(config.OfflineLLMSettings{
			BaseURL:    "http://127.0.0.1:8080/v1",
			Model:      "local-model",
			Template:   tpl,
			SourceLang: "English",
			TargetLang: "Arabic",
		})
		msgs := o.buildMessages("Let's go!", "Glossary: Valoria = فالوريا", got)
		if len(msgs) == 0 {
			t.Fatalf("template %s produced no messages", tpl)
		}
		joined := ""
		for _, m := range msgs {
			joined += m.Content + "\n"
		}
		if !strings.Contains(joined, "Current speaker: Aria") || !strings.Contains(joined, "Valoria") {
			t.Fatalf("template %s missing dialogue context or glossary:\n%s", tpl, joined)
		}
	}
}
