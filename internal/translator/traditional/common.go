// Package traditional implements free web translation providers (no API key).
package traditional

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"khutaf/internal/config"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

// Provider matches translator.Provider without introducing an import cycle.
type Provider interface {
	Name() string
	Translate(ctx context.Context, text string) (string, error)
}

// New constructs a traditional web translator by provider id, or nil if unknown.
func New(name string, cfg config.TraditionalSettings) Provider {
	switch name {
	case "google":
		return NewGoogle(cfg)
	case "microsoft":
		return NewMicrosoft(cfg)
	case "yandex":
		return NewYandex(cfg)
	case "modernmt":
		return NewModernMT(cfg)
	case "alibaba":
		return NewAlibaba(cfg)
	case "youdao":
		return NewYoudao(cfg)
	case "papago":
		return NewPapago(cfg)
	case "qqtransmart":
		return NewQQTranSmart(cfg)
	case "qqimt":
		return NewQQIMT(cfg)
	case "translatecom":
		return NewTranslateCom(cfg)
	case "volcengine":
		return NewVolcengine(cfg)
	case "lingocloud":
		return NewLingoCloud(cfg)
	default:
		return nil
	}
}

func newClient(timeout time.Duration, withCookies bool) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	c := &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}
	if withCookies {
		jar, _ := cookiejar.New(nil)
		c.Jar = jar
	}
	return c
}

func isAutoLang(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return l == "" || l == "auto" || l == "auto-detect"
}

func resolveSourceTarget(cfg config.TraditionalSettings, autoVal string) (string, string) {
	src := strings.TrimSpace(cfg.SourceLang)
	if isAutoLang(src) {
		src = autoVal
	}
	tgt := strings.TrimSpace(cfg.TargetLang)
	if tgt == "" {
		tgt = "ar"
	}
	return src, tgt
}

func doJSONPost(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return executeAndDecode(client, req, out)
}

func doFormPost(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return executeAndDecode(client, req, out)
}

func doGet(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return executeAndDecode(client, req, out)
}

func executeAndDecode(client *http.Client, req *http.Request, out any) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("invalid JSON: %.200s", raw)
	}
	return nil
}
