package traditional

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"khutaf/internal/config"
)

const volcengineTranslateURL = "https://translate.volcengine.com/crx/translate/v1/"

// Volcengine translates using ByteDance Volcengine's free extension endpoint.
type Volcengine struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewVolcengine creates a Volcengine translator client.
func NewVolcengine(cfg config.TraditionalSettings) *Volcengine {
	return &Volcengine{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (v *Volcengine) Name() string { return "volcengine" }

// Translate implements Provider.
func (v *Volcengine) Translate(ctx context.Context, text string) (string, error) {
	src, tgt := resolveSourceTarget(v.cfg, "")
	payload := map[string]any{
		"text":                 text,
		"target_language":      tgt,
		"enable_user_glossary": false,
		"glossary_list":        []any{},
		"category":             "",
	}
	if src != "" {
		payload["source_language"] = src
	}
	headers := map[string]string{
		"Origin": "chrome-extension://klgfhbiooeogdfodpopgppeadghjjemk",
	}
	var parsed struct {
		Translation string `json:"translation"`
	}
	if err := doJSONPost(ctx, v.client, volcengineTranslateURL, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("volcengine: %w", err)
	}
	if parsed.Translation == "" {
		return "", fmt.Errorf("volcengine: empty response")
	}
	return parsed.Translation, nil
}
