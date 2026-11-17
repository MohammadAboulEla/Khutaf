package traditional

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	translateComPageURL   = "https://www.translate.com/machine-translation"
	translateComDetectURL = "https://www.translate.com/translator/ajax_lang_auto_detect"
	translateComAPIURL    = "https://www.translate.com/translator/translate_mt"
)

// TranslateCom translates using Translate.com's free web endpoint.
type TranslateCom struct {
	cfg      config.TraditionalSettings
	client   *http.Client
	initOnce sync.Once
}

// NewTranslateCom creates a Translate.com translator client.
func NewTranslateCom(cfg config.TraditionalSettings) *TranslateCom {
	return &TranslateCom{cfg: cfg, client: newClient(15*time.Second, true)}
}

// Name implements Provider.
func (t *TranslateCom) Name() string { return "translatecom" }

// Translate implements Provider.
func (t *TranslateCom) Translate(ctx context.Context, text string) (string, error) {
	t.initOnce.Do(func() {
		_ = doGet(ctx, t.client, translateComPageURL, nil, nil)
	})
	src, tgt := resolveSourceTarget(t.cfg, "")
	if src == "" {
		var det struct {
			Language string `json:"language"`
		}
		if err := doFormPost(ctx, t.client, translateComDetectURL, nil, url.Values{"text_to_translate": {text}}, &det); err != nil {
			return "", fmt.Errorf("translatecom detect: %w", err)
		}
		src = det.Language
		if src == "" {
			src = "en"
		}
	}
	form := url.Values{
		"text_to_translate": {text},
		"source_lang":       {src},
		"translated_lang":   {tgt},
		"use_cache_only":    {"false"},
	}
	var parsed struct {
		TranslatedText string `json:"translated_text"`
	}
	if err := doFormPost(ctx, t.client, translateComAPIURL, nil, form, &parsed); err != nil {
		return "", fmt.Errorf("translatecom: %w", err)
	}
	if parsed.TranslatedText == "" {
		return "", fmt.Errorf("translatecom: empty response")
	}
	return html.UnescapeString(parsed.TranslatedText), nil
}
