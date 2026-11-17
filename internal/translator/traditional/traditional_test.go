package traditional

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"khutaf/internal/config"
)

func TestTraditionalFactory(t *testing.T) {
	names := []string{
		"google", "microsoft", "yandex", "modernmt",
		"alibaba", "youdao", "papago", "qqtransmart",
		"qqimt", "translatecom", "volcengine", "lingocloud",
	}
	cfg := config.TraditionalSettings{SourceLang: "auto", TargetLang: "ar"}
	for _, n := range names {
		p := New(n, cfg)
		if p == nil {
			t.Fatalf("New(%q) returned nil", n)
		}
		if p.Name() != n {
			t.Fatalf("Name() = %q, want %q", p.Name(), n)
		}
	}
	if New("unknown", cfg) != nil {
		t.Fatal("expected nil for unknown provider")
	}
}

func TestMicrosoftSignatureFormat(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	sig := microsoftSignature("api.cognitive.microsofttranslator.com/translate?api-version=3.0&to=ar", ts, "0123456789abcdef0123456789abcdef")
	parts := strings.Split(sig, "::")
	if len(parts) != 4 || parts[0] != "MSTranslatorAndroidApp" {
		t.Fatalf("unexpected signature format: %q", sig)
	}
	if parts[2] != "Fri, 02 Jan 2026 03:04:05 GMT" {
		t.Fatalf("unexpected date in signature: %q", parts[2])
	}
}

func TestModernMTAndYoudaoHashes(t *testing.T) {
	if h := modernMTVerify(1700000000000, "hello"); len(h) != 32 {
		t.Fatalf("unexpected md5 length: %q", h)
	}
	tm, sign := youdaoSign(1700000000000)
	if tm != "1700000000000" || len(sign) != 32 {
		t.Fatalf("unexpected youdao sign: (%q, %q)", tm, sign)
	}
}

func TestLingoDecrypt(t *testing.T) {
	plain := "مرحبا بالعالم"
	encoded := lingoRot13(base64.StdEncoding.EncodeToString([]byte(plain)))
	got, err := lingoDecrypt(encoded)
	if err != nil {
		t.Fatalf("lingoDecrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("lingoDecrypt = %q, want %q", got, plain)
	}
}
