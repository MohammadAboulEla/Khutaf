package translator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"khutaf/internal/config"
)

var reThinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// OfflineLLM translates using preconfigured prompt templates tailored to local
// offline translation GGUF models served via llama-server or Ollama.
type OfflineLLM struct {
	cfg    config.OfflineLLMSettings
	client *http.Client
}

// NewOfflineLLM creates an offline translation model client.
func NewOfflineLLM(cfg config.OfflineLLMSettings) *OfflineLLM {
	return &OfflineLLM{
		cfg: cfg,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: newHTTPTransport(),
		},
	}
}

// Name implements Provider.
func (o *OfflineLLM) Name() string {
	tpl := o.resolvedTemplate()
	model := strings.TrimSpace(o.cfg.Model)
	if model == "" {
		model = "local-model"
	}
	return "offline_llm:" + tpl + ":" + model
}

func (o *OfflineLLM) resolvedTemplate() string {
	tpl := strings.TrimSpace(o.cfg.Template)
	if tpl == "" || tpl == "auto" {
		m := strings.ToLower(o.cfg.Model)
		switch {
		case strings.Contains(m, "hy-mt"):
			return "hymt2"
		case strings.Contains(m, "translategemma"):
			return "translate_gemma"
		case strings.Contains(m, "nllb") || strings.Contains(m, "qwen"):
			return "nllb_instruct"
		default:
			return "generic_local"
		}
	}
	return tpl
}

// Translate implements Provider.
func (o *OfflineLLM) Translate(ctx context.Context, text string) (string, error) {
	baseURL := strings.TrimSpace(o.cfg.BaseURL)
	if baseURL == "" {
		return "", errors.New("offline LLM base URL is required")
	}
	model := strings.TrimSpace(o.cfg.Model)
	if model == "" {
		model = "local-model"
	}
	msgs := o.buildMessages(text, GlossaryPromptFromContext(ctx))
	out, err := doChatCompletion(ctx, o.client, baseURL, "", model, o.cfg.Temperature, msgs)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reThinkBlock.ReplaceAllString(out, "")), nil
}

func (o *OfflineLLM) buildMessages(text, glossaryPrompt string) []chatMessage {
	src := strings.TrimSpace(o.cfg.SourceLang)
	if src == "" {
		src = "English"
	}
	tgt := strings.TrimSpace(o.cfg.TargetLang)
	if tgt == "" {
		tgt = "Arabic"
	}

	switch o.resolvedTemplate() {
	case "hymt2":
		var content string
		if glossaryPrompt != "" {
			content = fmt.Sprintf(
				"Reference the following translations:\n%s\n\nTranslate the following text into %s. Note that you must ONLY output the translated result without any additional explanation:\n\n%s",
				glossaryPrompt, tgt, text,
			)
		} else {
			content = fmt.Sprintf(
				"Translate the following text into %s. Note that you should only output the translated result without any additional explanation:\n\n%s",
				tgt, text,
			)
		}
		return []chatMessage{{Role: "user", Content: content}}

	case "translate_gemma":
		content := fmt.Sprintf(
			"You are a professional %s to %s translator. Your goal is to accurately convey the meaning and nuances of the original %s text while adhering to %s grammar, vocabulary, and cultural sensitivities. Produce only the %s translation, without any additional explanations or commentary.",
			src, tgt, src, tgt, tgt,
		)
		if glossaryPrompt != "" {
			content += "\n\n" + glossaryPrompt
		}
		content += fmt.Sprintf("\n\nPlease translate the following %s text into %s:\n\n%s", src, tgt, text)
		return []chatMessage{{Role: "user", Content: content}}

	case "nllb_instruct":
		sys := fmt.Sprintf(
			"Translate the following %s game dialogue into %s. Keep all tags (<...>, [...], {...}) and placeholders unchanged. Output only the %s translation.",
			src, tgt, tgt,
		)
		if glossaryPrompt != "" {
			sys += "\n\n" + glossaryPrompt
		}
		return []chatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: text},
		}

	default: // generic_local
		sys := fmt.Sprintf(
			"You are a video game localizer. Translate from %s into natural %s. Keep every tag (<...>, [...], {...}) and placeholder exact. Output only the translation.",
			src, tgt,
		)
		if glossaryPrompt != "" {
			sys += "\n\n" + glossaryPrompt
		}
		return []chatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: text},
		}
	}
}
