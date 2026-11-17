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
	alibabaHostURL      = "https://translate.alibaba.com"
	alibabaCSRFURL      = "https://translate.alibaba.com/api/translate/csrftoken"
	alibabaTranslateURL = "https://translate.alibaba.com/api/translate/text"
)

// Alibaba translates using Alibaba Translate's free web endpoint.
type Alibaba struct {
	cfg    config.TraditionalSettings
	client *http.Client
	mu     sync.Mutex
	csrf   string
}

// NewAlibaba creates an Alibaba translator client.
func NewAlibaba(cfg config.TraditionalSettings) *Alibaba {
	return &Alibaba{cfg: cfg, client: newClient(15*time.Second, true)}
}

// Name implements Provider.
func (a *Alibaba) Name() string { return "alibaba" }

// Translate implements Provider.
func (a *Alibaba) Translate(ctx context.Context, text string) (string, error) {
	csrf, err := a.getCSRF(ctx, false)
	if err != nil {
		return "", err
	}
	out, err := a.postTranslate(ctx, csrf, text)
	if err == nil {
		return out, nil
	}
	csrf, cerr := a.getCSRF(ctx, true)
	if cerr != nil {
		return "", err
	}
	return a.postTranslate(ctx, csrf, text)
}

func (a *Alibaba) getCSRF(ctx context.Context, force bool) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.csrf != "" && !force {
		return a.csrf, nil
	}
	_ = doGet(ctx, a.client, alibabaHostURL, nil, nil)
	var parsed struct {
		Token string `json:"token"`
	}
	if err := doGet(ctx, a.client, alibabaCSRFURL, map[string]string{"Referer": alibabaHostURL}, &parsed); err != nil {
		return "", fmt.Errorf("alibaba csrf: %w", err)
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("alibaba csrf: empty token")
	}
	a.csrf = parsed.Token
	return a.csrf, nil
}

func (a *Alibaba) postTranslate(ctx context.Context, csrf, text string) (string, error) {
	src, tgt := resolveSourceTarget(a.cfg, "auto")
	q := url.Values{
		"srcLang": {src},
		"tgtLang": {tgt},
		"domain":  {"general"},
		"query":   {text},
		"_csrf":   {csrf},
	}
	var parsed struct {
		Data struct {
			TranslateText string `json:"translateText"`
		} `json:"data"`
	}
	if err := doFormPost(ctx, a.client, alibabaTranslateURL+"?"+q.Encode(), map[string]string{"Referer": alibabaHostURL}, url.Values{}, &parsed); err != nil {
		return "", fmt.Errorf("alibaba: %w", err)
	}
	if parsed.Data.TranslateText == "" {
		return "", fmt.Errorf("alibaba: empty response")
	}
	return html.UnescapeString(parsed.Data.TranslateText), nil
}
