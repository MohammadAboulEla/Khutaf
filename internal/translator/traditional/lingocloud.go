package traditional

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	lingoJWTURL       = "https://api.interpreter.caiyunai.com/v1/user/jwt/generate"
	lingoTranslateURL = "https://api.interpreter.caiyunai.com/v1/translator"
	lingoAuthToken    = "token:qgemv4jr1y38jyq6vhvi"
	lingoBrowserID    = "beba19f9d7f10c74c98334c9e8afcd34"
)

// LingoCloud translates using Caiyun LingoCloud's free web API.
type LingoCloud struct {
	cfg    config.TraditionalSettings
	client *http.Client
	mu     sync.Mutex
	jwt    string
}

// NewLingoCloud creates a LingoCloud translator client.
func NewLingoCloud(cfg config.TraditionalSettings) *LingoCloud {
	return &LingoCloud{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (l *LingoCloud) Name() string { return "lingocloud" }

// Translate implements Provider.
func (l *LingoCloud) Translate(ctx context.Context, text string) (string, error) {
	jwt, err := l.getJWT(ctx, false)
	if err != nil {
		return "", err
	}
	out, err := l.postTranslate(ctx, jwt, text)
	if err == nil {
		return out, nil
	}
	jwt, jerr := l.getJWT(ctx, true)
	if jerr != nil {
		return "", err
	}
	return l.postTranslate(ctx, jwt, text)
}

func (l *LingoCloud) getJWT(ctx context.Context, force bool) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.jwt != "" && !force {
		return l.jwt, nil
	}
	headers := map[string]string{
		"Origin":          "https://fanyi.caiyunapp.com",
		"Referer":         "https://fanyi.caiyunapp.com/",
		"app-name":        "xy",
		"os-type":         "web",
		"x-authorization": lingoAuthToken,
	}
	var parsed struct {
		JWT string `json:"jwt"`
	}
	if err := doJSONPost(ctx, l.client, lingoJWTURL, headers, map[string]string{"browser_id": lingoBrowserID}, &parsed); err != nil {
		return "", fmt.Errorf("lingocloud jwt: %w", err)
	}
	if parsed.JWT == "" {
		return "", fmt.Errorf("lingocloud jwt: empty token")
	}
	l.jwt = parsed.JWT
	return l.jwt, nil
}

func (l *LingoCloud) postTranslate(ctx context.Context, jwt, text string) (string, error) {
	src, tgt := resolveSourceTarget(l.cfg, "auto")
	headers := map[string]string{
		"Origin":          "https://fanyi.caiyunapp.com",
		"Referer":         "https://fanyi.caiyunapp.com/",
		"app-name":        "xy",
		"os-type":         "web",
		"t-authorization": jwt,
		"x-authorization": lingoAuthToken,
	}
	payload := map[string]any{
		"source":     text,
		"trans_type": src + "2" + tgt,
		"request_id": "web_fanyi",
		"media":      "text",
		"os_type":    "web",
		"dict":       true,
		"cached":     true,
		"replaced":   true,
		"detect":     true,
		"browser_id": lingoBrowserID,
	}
	var parsed struct {
		Target string `json:"target"`
	}
	if err := doJSONPost(ctx, l.client, lingoTranslateURL, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("lingocloud: %w", err)
	}
	if parsed.Target == "" {
		return "", fmt.Errorf("lingocloud: empty response")
	}
	dec, err := lingoDecrypt(parsed.Target)
	if err != nil {
		return "", fmt.Errorf("lingocloud decrypt: %w", err)
	}
	return dec, nil
}

func lingoRot13(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'A' && c <= 'Z':
			b[i] = 'A' + (c-'A'+13)%26
		case c >= 'a' && c <= 'z':
			b[i] = 'a' + (c-'a'+13)%26
		}
	}
	return string(b)
}

func lingoDecrypt(cipherText string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(lingoRot13(cipherText))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
