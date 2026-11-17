package traditional

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	modernMTPageURL = "https://www.modernmt.com/translate"
	modernMTAPIURL  = "https://webapi.modernmt.com/translate"
	modernMTSecret  = "webkey_E3sTuMjpP8Jez49GcYpDVH7r"
)

// ModernMT translates using ModernMT's free web API.
type ModernMT struct {
	cfg      config.TraditionalSettings
	client   *http.Client
	initOnce sync.Once
}

// NewModernMT creates a ModernMT translator client.
func NewModernMT(cfg config.TraditionalSettings) *ModernMT {
	return &ModernMT{cfg: cfg, client: newClient(15*time.Second, true)}
}

// Name implements Provider.
func (m *ModernMT) Name() string { return "modernmt" }

// Translate implements Provider.
func (m *ModernMT) Translate(ctx context.Context, text string) (string, error) {
	m.initOnce.Do(func() {
		_ = doGet(ctx, m.client, modernMTPageURL, map[string]string{"Referer": modernMTPageURL}, nil)
	})

	src, tgt := resolveSourceTarget(m.cfg, "")
	ts := time.Now().UnixMilli()
	payload := map[string]any{
		"q":         text,
		"source":    src,
		"target":    tgt,
		"ts":        ts,
		"verify":    modernMTVerify(ts, text),
		"hints":     "",
		"multiline": "true",
	}
	headers := map[string]string{
		"Origin":                   "https://www.modernmt.com",
		"Referer":                  modernMTPageURL,
		"X-Requested-With":         "XMLHttpRequest",
		"X-HTTP-Method-Override":   "GET",
	}
	var parsed struct {
		Data struct {
			Translation string `json:"translation"`
		} `json:"data"`
	}
	if err := doJSONPost(ctx, m.client, modernMTAPIURL, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("modernmt: %w", err)
	}
	if parsed.Data.Translation == "" {
		return "", fmt.Errorf("modernmt: empty response")
	}
	return parsed.Data.Translation, nil
}

func modernMTVerify(ts int64, text string) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s#%d#%s", modernMTSecret, ts, text)))
	return hex.EncodeToString(sum[:])
}
