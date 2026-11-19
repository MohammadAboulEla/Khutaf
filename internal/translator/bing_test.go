package translator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"khutaf/internal/config"
)

func TestBingSessionParsesIGAndIIDWithoutSwapping(t *testing.T) {
	var gotQuery string
	var gotKey, gotToken string

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Translator":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><script>IG:"IG_VAL_123";var params_AbusePreventionHelper = [1700000000,"tok123",3600];</script><div id="tta_outGDCont" data-iid="IID_VAL_456"></div></html>`))
		case "/ttranslatev3":
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			gotQuery = r.URL.RawQuery
			gotKey = r.FormValue("key")
			gotToken = r.FormValue("token")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"translations":[{"text":"مرحبا","to":"ar"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := newBing(config.BingSettings{SourceLang: "en", TargetLang: "ar"}, srv.URL+"/Translator", srv.URL+"/ttranslatev3", false)
	out, err := b.Translate(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	if out != "مرحبا" {
		t.Fatalf("Translate = %q, want %q", out, "مرحبا")
	}
	if !strings.Contains(gotQuery, "IG=IG_VAL_123") || !strings.Contains(gotQuery, "IID=IID_VAL_456") {
		t.Fatalf("query params swapped or missing: %q", gotQuery)
	}
	if gotKey != "1700000000" || gotToken != "tok123" {
		t.Fatalf("form auth mismatch: key=%q token=%q", gotKey, gotToken)
	}
}

func TestBingGetRedirectsFollowAndCapAt10(t *testing.T) {
	t.Run("follows_single_redirect", func(t *testing.T) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/Translator":
				http.Redirect(w, r, "/Translator2", http.StatusFound)
			case "/Translator2":
				_, _ = w.Write([]byte(`<html><script>IG:"IG1";var params_AbusePreventionHelper = [1,"t",1];</script><div id="tta_outGDCont" data-iid="IID1"></div></html>`))
			case "/ttranslatev3":
				_, _ = w.Write([]byte(`[{"translations":[{"text":"تم"}]}]`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		b := newBing(config.BingSettings{}, srv.URL+"/Translator", srv.URL+"/ttranslatev3", false)
		out, err := b.Translate(context.Background(), "Done")
		if err != nil || out != "تم" {
			t.Fatalf("Translate after redirect = (%q, %v), want (%q, nil)", out, err, "تم")
		}
	})

	t.Run("stops_at_10_redirects", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := hits.Add(1)
			http.Redirect(w, r, fmt.Sprintf("/Translator?hop=%d", n), http.StatusFound)
		}))
		defer srv.Close()

		b := newBing(config.BingSettings{}, srv.URL+"/Translator", srv.URL+"/ttranslatev3", false)
		_, err := b.Translate(context.Background(), "Loop")
		if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
			t.Fatalf("expected redirect cap error, got: %v", err)
		}
		if gotHits := hits.Load(); gotHits > 11 {
			t.Fatalf("expected at most 11 requests before stopping, got %d", gotHits)
		}
	})
}
