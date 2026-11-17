package traditional

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"khutaf/internal/config"
)

const (
	googleTranslateURL = "https://translate-pa.googleapis.com/v1/translateHtml"
	googleAPIKey       = "AIzaSyATBXajvzQLTDHEQbcpq0Ihe0vWDHmO520"
)

// Google translates using Google Translate's free web-pa endpoint.
type Google struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewGoogle creates a Google translator client.
func NewGoogle(cfg config.TraditionalSettings) *Google {
	return &Google{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (g *Google) Name() string { return "google" }

// Translate implements Provider.
func (g *Google) Translate(ctx context.Context, text string) (string, error) {
	lines := strings.Split(text, "\n")
	out := make([]string, len(lines))
	src, tgt := resolveSourceTarget(g.cfg, "auto")
	for i, line := range lines {
		if line == "" {
			continue
		}
		tr, err := g.translateLine(ctx, line, src, tgt)
		if err != nil {
			return "", err
		}
		out[i] = tr
	}
	return strings.Join(out, "\n"), nil
}

func (g *Google) translateLine(ctx context.Context, line, src, tgt string) (string, error) {
	payload := []any{[]any{[]string{line}, src, tgt}, "wt_lib"}
	headers := map[string]string{
		"Content-Type":    "application/json+protobuf",
		"X-Goog-Api-Key":  googleAPIKey,
		"Accept":          "*/*",
		"Accept-Language": "en-US,en;q=0.9",
	}
	var parsed [][]string
	if err := doJSONPost(ctx, g.client, googleTranslateURL, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("google: %w", err)
	}
	if len(parsed) == 0 || len(parsed[0]) == 0 {
		return "", fmt.Errorf("google: empty response")
	}
	return html.UnescapeString(parsed[0][0]), nil
}
