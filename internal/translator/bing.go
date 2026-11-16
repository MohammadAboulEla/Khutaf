package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"khutaf/internal/config"
)

const (
	bingHostURL      = "https://www.bing.com/Translator"
	bingTranslateURL = "https://www.bing.com/ttranslatev3"
	bingUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	reBingAbuse = regexp.MustCompile(`var params_AbusePreventionHelper\s*=\s*(\[[^\]]+\]);`)
	reBingIID   = regexp.MustCompile(`<div\s+id="tta_outGDCont"\s+data-iid="([^"]+)"`)
	reBingIG    = regexp.MustCompile(`IG:"([^"]+)"`)
)

type bingSession struct {
	key   string
	token string
	ig    string
	iid   string
}

// Bing translates using Bing's free web translator endpoint (no API key).
type Bing struct {
	cfg    config.BingSettings
	client *http.Client

	mu   sync.Mutex
	sess *bingSession
}

// NewBing creates a free Bing translator client and pre-warms its session.
func NewBing(cfg config.BingSettings) *Bing {
	jar, _ := cookiejar.New(nil)
	b := &Bing{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
			Jar:     jar,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = b.getSession(ctx, false)
	}()
	return b
}

// Name implements Provider.
func (b *Bing) Name() string { return "bing" }

// Translate implements Provider.
func (b *Bing) Translate(ctx context.Context, text string) (string, error) {
	sess, err := b.getSession(ctx, false)
	if err != nil {
		return "", err
	}
	out, err := b.postTranslate(ctx, sess, text)
	if err == nil {
		return out, nil
	}
	// Token/IG may have expired; refresh once and retry.
	sess, rerr := b.getSession(ctx, true)
	if rerr != nil {
		return "", err
	}
	return b.postTranslate(ctx, sess, text)
}

func (b *Bing) getSession(ctx context.Context, force bool) (*bingSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sess != nil && !force {
		return b.sess, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bingHostURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", bingUserAgent)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing init: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("bing init read: %w", err)
	}
	html := string(raw)

	abuseMatch := reBingAbuse.FindStringSubmatch(html)
	if len(abuseMatch) < 2 {
		return nil, errors.New("bing init: missing abuse prevention token")
	}
	var params []any
	if err := json.Unmarshal([]byte(abuseMatch[1]), &params); err != nil || len(params) < 2 {
		return nil, errors.New("bing init: invalid token payload")
	}
	key := formatJSONScalar(params[0])
	token, _ := params[1].(string)
	if key == "" || token == "" {
		return nil, errors.New("bing init: empty key or token")
	}

	var ig, iid string
	if m := reBingIID.FindStringSubmatch(html); len(m) >= 2 {
		ig = m[1]
	}
	if m := reBingIG.FindStringSubmatch(html); len(m) >= 2 {
		iid = m[1]
	}

	b.sess = &bingSession{key: key, token: token, ig: ig, iid: iid}
	return b.sess, nil
}

func (b *Bing) postTranslate(ctx context.Context, sess *bingSession, text string) (string, error) {
	from := strings.TrimSpace(b.cfg.SourceLang)
	if from == "" {
		from = "auto-detect"
	}
	to := strings.TrimSpace(b.cfg.TargetLang)
	if to == "" {
		to = "ar"
	}

	form := url.Values{
		"text":                                {text},
		"fromLang":                            {from},
		"to":                                  {to},
		"tryFetchingGenderDebiasedTranslations": {"true"},
		"key":                                 {sess.key},
		"token":                               {sess.token},
	}

	q := url.Values{"isVertical": {"1"}}
	if sess.ig != "" {
		q.Set("IG", sess.ig)
	}
	if sess.iid != "" {
		q.Set("IID", sess.iid)
	}

	targetURL := bingTranslateURL + "?" + q.Encode()
	resp, raw, err := b.doPostForm(ctx, targetURL, form)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		if loc := resp.Header.Get("Location"); loc != "" {
			if u, uerr := resp.Request.URL.Parse(loc); uerr == nil {
				resp, raw, err = b.doPostForm(ctx, u.String(), form)
				if err != nil {
					return "", err
				}
			}
		}
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bing HTTP %d: %.200s", resp.StatusCode, raw)
	}

	var parsed []struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed) == 0 || len(parsed[0].Translations) == 0 {
		return "", fmt.Errorf("bing invalid response: %.200s", raw)
	}
	return parsed[0].Translations[0].Text, nil
}

func (b *Bing) doPostForm(ctx context.Context, targetURL string, form url.Values) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", bingUserAgent)
	req.Header.Set("Referer", bingHostURL)
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return resp, raw, err
}

func formatJSONScalar(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return fmt.Sprint(t)
	}
}
