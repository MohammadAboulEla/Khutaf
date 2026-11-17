package traditional

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	qqTransmartHostURL = "https://transmart.qq.com"
	qqTransmartAPIURL  = "https://transmart.qq.com/api/imt"
)

// QQTranSmart translates using Tencent TranSmart with sentence segmentation.
type QQTranSmart struct {
	cfg      config.TraditionalSettings
	client   *http.Client
	initOnce sync.Once
	split    bool
	name     string
}

// NewQQTranSmart creates a qqTranSmart translator client.
func NewQQTranSmart(cfg config.TraditionalSettings) *QQTranSmart {
	return &QQTranSmart{
		cfg:    cfg,
		client: newClient(15*time.Second, true),
		split:  true,
		name:   "qqtransmart",
	}
}

// NewQQIMT creates a direct single-request qqimt translator client.
func NewQQIMT(cfg config.TraditionalSettings) *QQTranSmart {
	return &QQTranSmart{
		cfg:    cfg,
		client: newClient(15*time.Second, false),
		split:  false,
		name:   "qqimt",
	}
}

// Name implements Provider.
func (q *QQTranSmart) Name() string { return q.name }

// Translate implements Provider.
func (q *QQTranSmart) Translate(ctx context.Context, text string) (string, error) {
	if q.split {
		q.initOnce.Do(func() {
			_ = doGet(ctx, q.client, qqTransmartHostURL, nil, nil)
		})
	}
	src, tgt := resolveSourceTarget(q.cfg, "auto")
	clientKey := fmt.Sprintf("browser-chrome-124.0.0-Windows_10-%s-%d", randomHex16(), time.Now().UnixMilli())

	segments := []string{text}
	if q.split {
		if splitSegs, err := q.analyzeText(ctx, clientKey, text); err == nil && len(splitSegs) > 0 {
			segments = splitSegs
		}
	}

	textList := make([]string, 0, len(segments)+2)
	textList = append(textList, "")
	textList = append(textList, segments...)
	textList = append(textList, "")

	payload := map[string]any{
		"header": map[string]any{
			"fn":         "auto_translation",
			"session":    "",
			"client_key": clientKey,
			"user":       "",
		},
		"type":           "plain",
		"model_category": "normal",
		"text_domain":    "general",
		"source": map[string]any{
			"lang":      src,
			"text_list": textList,
		},
		"target": map[string]any{
			"lang": tgt,
		},
	}
	headers := map[string]string{
		"Origin":           qqTransmartHostURL,
		"Referer":          qqTransmartHostURL + "/",
		"X-Requested-With": "XMLHttpRequest",
	}
	if q.split {
		headers["Cookie"] = "client_key=" + clientKey
	}

	var parsed struct {
		AutoTranslation []string `json:"auto_translation"`
	}
	if err := doJSONPost(ctx, q.client, qqTransmartAPIURL, headers, payload, &parsed); err != nil {
		return "", fmt.Errorf("%s: %w", q.name, err)
	}
	if len(parsed.AutoTranslation) == 0 {
		return "", fmt.Errorf("%s: empty response", q.name)
	}
	return strings.Join(parsed.AutoTranslation, ""), nil
}

func (q *QQTranSmart) analyzeText(ctx context.Context, clientKey, text string) ([]string, error) {
	payload := map[string]any{
		"header": map[string]any{
			"fn":         "text_analysis",
			"client_key": clientKey,
		},
		"type": "plain",
		"text": text,
		"normalize": map[string]string{
			"merge_broken_line": "false",
		},
	}
	var parsed struct {
		Text         string `json:"text"`
		SentenceList []struct {
			Start int `json:"start"`
			Len   int `json:"len"`
		} `json:"sentence_list"`
	}
	if err := doJSONPost(ctx, q.client, qqTransmartAPIURL, nil, payload, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.SentenceList) == 0 || parsed.Text == "" {
		return nil, nil
	}
	runes := []rune(parsed.Text)
	out := make([]string, 0, len(parsed.SentenceList))
	for _, s := range parsed.SentenceList {
		if s.Start < 0 || s.Start+s.Len > len(runes) {
			continue
		}
		out = append(out, string(runes[s.Start:s.Start+s.Len]))
	}
	return out, nil
}
