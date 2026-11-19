package hook

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

// Wire protocol shared with native/khutaf-proto (keep both in sync).
// Frame: u32 len | u8 type | payload (little-endian, UTF-16 strings).

const protoVersion = 2

const maxFrame = 16 * 1024 * 1024

const (
	msgHello         byte = 0x01
	msgHookAdded     byte = 0x02
	msgHookRemoved   byte = 0x03
	msgText          byte = 0x04
	msgEmbedRequest  byte = 0x05
	msgLog           byte = 0x06
	msgSettings      byte = 0x81
	msgSetEmbed      byte = 0x82
	msgEmbedResponse byte = 0x83
	msgDetach        byte = 0x84
)

// Log levels sent by the DLL.
const (
	logDebug byte = iota
	logInfo
	logWarn
	logError
)

// wireMsg is a decoded frame; only the fields relevant to Kind are set.
type wireMsg struct {
	Kind         byte
	Proto, PID   uint32
	Arch         byte
	Engine       string
	UnityVersion string
	HookID       uint64
	ReqID        uint64
	Ctx, Ctx2    uint64
	Code, Name   string
	Text         string
	HasText      bool // EmbedResponse: false keeps the original text
	Embeddable   bool
	On           bool
	Level        byte
	Settings     wireSettings
}

type wireSettings struct {
	WaitMs, DisplayMode                    uint32
	FontFamily, FontDir, PendingText       string
	ClearText, ChangeSize, StripTypewriter bool
	NonBlocking                            bool
	FontScale                              float32
}

type encoder struct{ b []byte }

func (e *encoder) u8(v byte) { e.b = append(e.b, v) }
func (e *encoder) boolean(v bool) {
	if v {
		e.u8(1)
	} else {
		e.u8(0)
	}
}
func (e *encoder) u32(v uint32) { e.b = binary.LittleEndian.AppendUint32(e.b, v) }
func (e *encoder) u64(v uint64) { e.b = binary.LittleEndian.AppendUint64(e.b, v) }
func (e *encoder) str(s string) {
	units := utf16.Encode([]rune(s))
	e.u32(uint32(len(units)))
	for _, u := range units {
		e.b = binary.LittleEndian.AppendUint16(e.b, u)
	}
}

// encode returns the full frame including the length prefix.
func (m *wireMsg) encode() []byte {
	e := &encoder{b: make([]byte, 4, 64)}
	e.u8(m.Kind)
	switch m.Kind {
	case msgHello:
		e.u32(m.Proto)
		e.u32(m.PID)
		e.u8(m.Arch)
		e.str(m.Engine)
		e.str(m.UnityVersion)
	case msgHookAdded:
		e.u64(m.HookID)
		e.str(m.Code)
		e.str(m.Name)
		e.boolean(m.Embeddable)
	case msgHookRemoved:
		e.u64(m.HookID)
	case msgText:
		e.u64(m.HookID)
		e.u64(m.Ctx)
		e.u64(m.Ctx2)
		e.str(m.Text)
	case msgEmbedRequest:
		e.u64(m.ReqID)
		e.u64(m.HookID)
		e.u64(m.Ctx)
		e.u64(m.Ctx2)
		e.str(m.Text)
	case msgLog:
		e.u8(m.Level)
		e.str(m.Text)
	case msgSettings:
		s := m.Settings
		e.u32(s.WaitMs)
		e.u32(s.DisplayMode)
		e.str(s.FontFamily)
		e.str(s.FontDir)
		e.boolean(s.ClearText)
		e.boolean(s.ChangeSize)
		e.u32(math.Float32bits(s.FontScale))
		e.boolean(s.StripTypewriter)
		e.boolean(s.NonBlocking)
		e.str(s.PendingText)
	case msgSetEmbed:
		e.u64(m.HookID)
		e.u64(m.Ctx)
		e.u64(m.Ctx2)
		e.boolean(m.On)
	case msgEmbedResponse:
		e.u64(m.ReqID)
		e.boolean(m.HasText)
		if m.HasText {
			e.str(m.Text)
		}
	case msgDetach:
	}
	binary.LittleEndian.PutUint32(e.b[:4], uint32(len(e.b)-4))
	return e.b
}

var errTruncated = errors.New("truncated frame")

type decoder struct {
	b   []byte
	err error
}

func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n < 0 || len(d.b) < n {
		d.err = errTruncated
		return nil
	}
	s := d.b[:n]
	d.b = d.b[n:]
	return s
}

func (d *decoder) u8() byte {
	if s := d.take(1); s != nil {
		return s[0]
	}
	return 0
}
func (d *decoder) boolean() bool { return d.u8() != 0 }
func (d *decoder) u32() uint32 {
	if s := d.take(4); s != nil {
		return binary.LittleEndian.Uint32(s)
	}
	return 0
}
func (d *decoder) u64() uint64 {
	if s := d.take(8); s != nil {
		return binary.LittleEndian.Uint64(s)
	}
	return 0
}
func (d *decoder) str() string {
	n := d.u32()
	if uint64(n)*2 > uint64(len(d.b)) {
		d.err = errTruncated
		return ""
	}
	raw := d.take(int(n) * 2)
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units))
}

// decodeBody decodes a frame body (type byte + payload).
func decodeBody(body []byte) (*wireMsg, error) {
	d := &decoder{b: body}
	m := &wireMsg{Kind: d.u8()}
	switch m.Kind {
	case msgHello:
		m.Proto, m.PID, m.Arch = d.u32(), d.u32(), d.u8()
		m.Engine, m.UnityVersion = d.str(), d.str()
	case msgHookAdded:
		m.HookID = d.u64()
		m.Code, m.Name = d.str(), d.str()
		m.Embeddable = d.boolean()
	case msgHookRemoved:
		m.HookID = d.u64()
	case msgText:
		m.HookID, m.Ctx, m.Ctx2 = d.u64(), d.u64(), d.u64()
		m.Text = d.str()
	case msgEmbedRequest:
		m.ReqID, m.HookID, m.Ctx, m.Ctx2 = d.u64(), d.u64(), d.u64(), d.u64()
		m.Text = d.str()
	case msgLog:
		m.Level = d.u8()
		m.Text = d.str()
	case msgSettings:
		s := &m.Settings
		s.WaitMs, s.DisplayMode = d.u32(), d.u32()
		s.FontFamily, s.FontDir = d.str(), d.str()
		s.ClearText, s.ChangeSize = d.boolean(), d.boolean()
		s.FontScale = math.Float32frombits(d.u32())
		s.StripTypewriter = d.boolean()
		s.NonBlocking = d.boolean()
		s.PendingText = d.str()
	case msgSetEmbed:
		m.HookID, m.Ctx, m.Ctx2 = d.u64(), d.u64(), d.u64()
		m.On = d.boolean()
	case msgEmbedResponse:
		m.ReqID = d.u64()
		if m.HasText = d.boolean(); m.HasText {
			m.Text = d.str()
		}
	case msgDetach:
	default:
		if d.err == nil {
			return nil, fmt.Errorf("unknown message type 0x%02x", m.Kind)
		}
	}
	if d.err != nil {
		return nil, d.err
	}
	if len(d.b) != 0 {
		return nil, fmt.Errorf("trailing %d bytes in frame", len(d.b))
	}
	return m, nil
}

func writeMsg(w io.Writer, m *wireMsg) error {
	_, err := w.Write(m.encode())
	return err
}

func readMsg(r io.Reader) (*wireMsg, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || n > maxFrame {
		return nil, fmt.Errorf("bad frame length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return decodeBody(body)
}
