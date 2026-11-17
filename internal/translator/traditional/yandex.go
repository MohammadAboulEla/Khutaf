package traditional

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"khutaf/internal/config"
)

const (
	yandexDetectURL    = "https://translate.yandex.net/api/v1/tr.json/detect"
	yandexTranslateURL = "https://browser.translate.yandex.net/api/v1/tr.json/translate"
)

// Yandex translates using Yandex's browser translation endpoint.
type Yandex struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewYandex creates a Yandex translator client.
func NewYandex(cfg config.TraditionalSettings) *Yandex {
	return &Yandex{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (y *Yandex) Name() string { return "yandex" }

// Translate implements Provider.
func (y *Yandex) Translate(ctx context.Context, text string) (string, error) {
	src, tgt := resolveSourceTarget(y.cfg, "")
	if src == "" {
		detectQ := url.Values{
			"srv":  {"browser_video_translation"},
			"text": {text},
		}
		var det struct {
			Lang string `json:"lang"`
		}
		if err := doGet(ctx, y.client, yandexDetectURL+"?"+detectQ.Encode(), nil, &det); err != nil {
			return "", fmt.Errorf("yandex detect: %w", err)
		}
		src = det.Lang
		if src == "" {
			src = "en"
		}
	}

	q := url.Values{
		"lang": {src + "-" + tgt},
		"text": {text},
		"srv":  {"browser_video_translation"},
	}
	form := url.Values{
		"maxRetryCount":     {"2"},
		"fetchAbortTimeout": {"500"},
	}
	var parsed struct {
		Text []string `json:"text"`
	}
	if err := doFormPost(ctx, y.client, yandexTranslateURL+"?"+q.Encode(), nil, form, &parsed); err != nil {
		return "", fmt.Errorf("yandex: %w", err)
	}
	if len(parsed.Text) == 0 {
		return "", fmt.Errorf("yandex: empty response")
	}
	return parsed.Text[0], nil
}
