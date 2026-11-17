package traditional

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"khutaf/internal/config"
)

const (
	youdaoTranslateURL = "https://dict.youdao.com/dicttranslate"
	youdaoSignKey      = "cybibtzhdwayqjmrncst"
)

// Youdao translates using Youdao Desktop Dict's free translation endpoint.
type Youdao struct {
	cfg    config.TraditionalSettings
	client *http.Client
}

// NewYoudao creates a Youdao translator client.
func NewYoudao(cfg config.TraditionalSettings) *Youdao {
	return &Youdao{cfg: cfg, client: newClient(15*time.Second, false)}
}

// Name implements Provider.
func (y *Youdao) Name() string { return "youdao" }

// Translate implements Provider.
func (y *Youdao) Translate(ctx context.Context, text string) (string, error) {
	src, tgt := resolveSourceTarget(y.cfg, "auto")
	tm, sign := youdaoSign(time.Now().UnixMilli())
	q := url.Values{
		"keyfrom":      {"deskdict.main"},
		"client":       {"deskdict"},
		"from":         {src},
		"to":           {tgt},
		"keyid":        {"deskdict"},
		"mysticTime":   {tm},
		"pointParam":   {"client,product,mysticTime"},
		"sign":         {sign},
		"domain":       {"0"},
		"useTerm":      {"false"},
		"noCheckPrivate": {"false"},
		"recTerms":     {"[]"},
		"id":           {"0a464aedddbc6e4b9"},
		"vendor":       {"fanyiweb_navigation"},
		"in":           {"YoudaoDict_fanyiweb_navigation"},
		"appVer":       {"11.2.0.0"},
		"appZengqiang": {"0"},
		"abTest":       {"0"},
		"model":        {"LENOVO"},
		"screen":       {"1920*1080"},
		"OsVersion":    {"10.0.19045"},
		"network":      {"none"},
		"mid":          {"windows10.0.19045"},
		"appVersion":   {"11.2.0.0"},
		"product":      {"deskdict"},
		"source":       {"mine_transtab_realtime"},
	}
	form := url.Values{"i": {text}}
	var parsed struct {
		TranslateResult [][]struct {
			Tgt string `json:"tgt"`
		} `json:"translateResult"`
	}
	if err := doFormPost(ctx, y.client, youdaoTranslateURL+"?"+q.Encode(), nil, form, &parsed); err != nil {
		return "", fmt.Errorf("youdao: %w", err)
	}
	if len(parsed.TranslateResult) == 0 {
		return "", fmt.Errorf("youdao: empty response")
	}
	var b strings.Builder
	for _, para := range parsed.TranslateResult {
		for _, seg := range para {
			b.WriteString(seg.Tgt)
		}
	}
	return b.String(), nil
}

func youdaoSign(nowMs int64) (string, string) {
	tm := strconv.FormatInt(nowMs, 10)
	raw := fmt.Sprintf("client=deskdict&mysticTime=%s&product=deskdict&key=%s", tm, youdaoSignKey)
	sum := md5.Sum([]byte(raw))
	return tm, hex.EncodeToString(sum[:])
}
