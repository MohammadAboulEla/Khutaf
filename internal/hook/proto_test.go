//go:build windows

package hook

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"
)

// Golden bytes shared with native/khutaf-proto/src/lib.rs.
func TestProtoGolden(t *testing.T) {
	hello := &wireMsg{Kind: msgHello, Proto: 1, PID: 1234, Arch: 64, Engine: "ab"}
	if got := hex.EncodeToString(hello.encode()); got != "160000000101000000d204000040020000006100620000000000" {
		t.Fatalf("hello = %s", got)
	}
	resp := &wireMsg{Kind: msgEmbedResponse, ReqID: 9, HasText: true, Text: "ع"}
	if got := hex.EncodeToString(resp.encode()); got != "1000000083090000000000000001010000003906" {
		t.Fatalf("embed response = %s", got)
	}
}

func TestProtoRoundTrip(t *testing.T) {
	msgs := []*wireMsg{
		{Kind: msgHello, Proto: 1, PID: 7, Arch: 64, Engine: "unity-mono", UnityVersion: "2021.3"},
		{Kind: msgHookAdded, HookID: 3, Code: "UNITY:TMP_Text.set_text", Name: "TMP", Embeddable: true},
		{Kind: msgHookRemoved, HookID: 3},
		{Kind: msgText, HookID: 3, Ctx: 1, Ctx2: 2, Text: "Hello"},
		{Kind: msgEmbedRequest, ReqID: 5, HookID: 3, Text: "مرحبا 😀"},
		{Kind: msgLog, Level: logWarn, Text: "x"},
		{Kind: msgSettings, Settings: wireSettings{WaitMs: 1000, DisplayMode: 1, FontFamily: "Arial", FontDir: `C:\f`, ClearText: true, FontScale: 1.5}},
		{Kind: msgSetEmbed, HookID: 3, On: true},
		{Kind: msgEmbedResponse, ReqID: 5, HasText: true, Text: "ok"},
		{Kind: msgEmbedResponse, ReqID: 5},
		{Kind: msgDetach},
	}
	for _, m := range msgs {
		var buf bytes.Buffer
		if err := writeMsg(&buf, m); err != nil {
			t.Fatal(err)
		}
		got, err := readMsg(&buf)
		if err != nil {
			t.Fatalf("kind 0x%02x: %v", m.Kind, err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Fatalf("kind 0x%02x: got %+v want %+v", m.Kind, got, m)
		}
	}
}

func TestProtoRejectsTruncated(t *testing.T) {
	if _, err := decodeBody([]byte{msgHookRemoved, 1, 2}); err == nil {
		t.Fatal("expected truncation error")
	}
	if _, err := decodeBody([]byte{0x55}); err == nil {
		t.Fatal("expected unknown type error")
	}
}
