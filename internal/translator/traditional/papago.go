package traditional

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"khutaf/internal/config"
)

const papagoTranslateURL = "https://papago.naver.com/api/text/translation"

// Papago translates using Naver Papago's web API.
type Papago struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewPapago creates a Papago translator client.
func NewPapago(cfg config.TraditionalSettings) *Papago {
	return &Papago{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (p *Papago) Name() string { return "papago" }

// Translate implements Provider.
func (p *Papago) Translate(ctx context.Context, text string) (string, error) {
	src, tgt := resolveSourceTarget(p.cfg, "auto")
	form := url.Values{
		"dict":        {"true"},
		"dictDisplay": {"30"},
		"honorific":   {"false"},
		"useGlossary": {"false"},
		"source":      {src},
		"target":      {tgt},
		"text":        {text},
	}
	var parsed struct {
		TranslatedText string `json:"translatedText"`
	}
	if err := doFormPost(ctx, p.client, papagoTranslateURL, nil, form, &parsed); err != nil {
		return "", fmt.Errorf("papago: %w", err)
	}
	if parsed.TranslatedText == "" {
		return "", fmt.Errorf("papago: empty response")
	}
	return parsed.TranslatedText, nil
}
