package traditional

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"khutaf/internal/config"
)

const microsoftEndpoint = "api.cognitive.microsofttranslator.com"

var microsoftPrivateKey = []byte{
	0xa2, 0x29, 0x3a, 0x3d, 0xd0, 0xdd, 0x32, 0x73,
	0x97, 0x7a, 0x64, 0xdb, 0xc2, 0xf3, 0x27, 0xf5,
	0xd7, 0xbf, 0x87, 0xd9, 0x45, 0x9d, 0xf0, 0x5a,
	0x09, 0x66, 0xc6, 0x30, 0xc6, 0x6a, 0xaa, 0x84,
	0x9a, 0x41, 0xaa, 0x94, 0x3a, 0xa8, 0xd5, 0x1a,
	0x6e, 0x4d, 0xaa, 0xc9, 0xa3, 0x70, 0x12, 0x35,
	0xc7, 0xeb, 0x12, 0xf6, 0xe8, 0x23, 0x07, 0x9e,
	0x47, 0x10, 0x95, 0x91, 0x88, 0x55, 0xd8, 0x17,
}

// Microsoft translates using Microsoft Translator's free signed endpoint.
type Microsoft struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewMicrosoft creates a Microsoft translator client.
func NewMicrosoft(cfg config.TraditionalSettings) *Microsoft {
	return &Microsoft{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (m *Microsoft) Name() string { return "microsoft" }

// Translate implements Provider.
func (m *Microsoft) Translate(ctx context.Context, text string) (string, error) {
	src, tgt := resolveSourceTarget(m.cfg, "")
	path := fmt.Sprintf("%s/translate?api-version=3.0&to=%s", microsoftEndpoint, url.QueryEscape(tgt))
	if src != "" {
		path += "&from=" + url.QueryEscape(src)
	}
	sig := microsoftSignature(path, time.Now().UTC(), randomHex16())
	headers := map[string]string{
		"X-MT-Signature": sig,
	}
	payload := []map[string]string{{"Text": text}}
	var parsed []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := doJSONPost(ctx, m.client, "https://"+path, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("microsoft: %w", err)
	}
	if len(parsed) == 0 || len(parsed[0].Translations) == 0 {
		return "", fmt.Errorf("microsoft: empty response")
	}
	return parsed[0].Translations[0].Text, nil
}

func microsoftSignature(rawURL string, now time.Time, guid string) string {
	escapedURL := strings.ReplaceAll(url.QueryEscape(rawURL), "+", "%20")
	dateStr := now.UTC().Format("Mon, 02 Jan 2006 15:04:05") + " GMT"
	msg := strings.ToLower(fmt.Sprintf("MSTranslatorAndroidApp%s%s%s", escapedURL, dateStr, guid))
	mac := hmac.New(sha256.New, microsoftPrivateKey)
	mac.Write([]byte(msg))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("MSTranslatorAndroidApp::%s::%s::%s", sig, dateStr, guid)
}

func randomHex16() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
