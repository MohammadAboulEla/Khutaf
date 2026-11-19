//! Khutaf wire protocol shared by the Rust hook DLL and the Go host
//! (`internal/hook/proto.go` mirrors this file; keep both in sync).
//!
//! Frame: `u32 len | u8 type | payload` where `len` counts the type byte and
//! the payload. All integers are little-endian. Strings are `u32 count` UTF-16
//! code units followed by the units. Booleans are one byte (0/1).

use std::io::{self, Read, Write};

pub const PROTO_VERSION: u32 = 2;

/// Upper bound on a single frame, guards against corrupted length prefixes.
pub const MAX_FRAME: u32 = 16 * 1024 * 1024;

pub mod kind {
    // DLL -> host
    pub const HELLO: u8 = 0x01;
    pub const HOOK_ADDED: u8 = 0x02;
    pub const HOOK_REMOVED: u8 = 0x03;
    pub const TEXT: u8 = 0x04;
    pub const EMBED_REQUEST: u8 = 0x05;
    pub const LOG: u8 = 0x06;
    // host -> DLL
    pub const SETTINGS: u8 = 0x81;
    pub const SET_EMBED: u8 = 0x82;
    pub const EMBED_RESPONSE: u8 = 0x83;
    pub const DETACH: u8 = 0x84;
}

pub mod level {
    pub const DEBUG: u8 = 0;
    pub const INFO: u8 = 1;
    pub const WARN: u8 = 2;
    pub const ERROR: u8 = 3;
}

#[derive(Debug, Clone, PartialEq, Default)]
pub struct Settings {
    pub wait_ms: u32,
    pub display_mode: u32,
    pub font_family: String,
    pub font_dir: String,
    pub clear_text: bool,
    pub change_font_size: bool,
    pub font_scale: f32,
    pub strip_typewriter_tags: bool,
    pub non_blocking: bool,
    pub pending_text: String,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Msg {
    Hello { proto: u32, pid: u32, arch: u8, engine: String, unity_version: String },
    /// `code` is stable across runs (used for saved profiles); `name` is for display.
    HookAdded { hook_id: u64, code: String, name: String, embeddable: bool },
    HookRemoved { hook_id: u64 },
    Text { hook_id: u64, ctx: u64, ctx2: u64, text: String },
    EmbedRequest { req_id: u64, hook_id: u64, ctx: u64, ctx2: u64, text: String },
    Log { level: u8, msg: String },
    Settings(Settings),
    SetEmbed { hook_id: u64, ctx: u64, ctx2: u64, on: bool },
    /// `None` keeps the original text.
    EmbedResponse { req_id: u64, text: Option<String> },
    Detach,
}

// ---- encoding ----

struct Enc(Vec<u8>);

impl Enc {
    fn u8(&mut self, v: u8) { self.0.push(v); }
    fn bool(&mut self, v: bool) { self.0.push(v as u8); }
    fn u32(&mut self, v: u32) { self.0.extend_from_slice(&v.to_le_bytes()); }
    fn u64(&mut self, v: u64) { self.0.extend_from_slice(&v.to_le_bytes()); }
    fn f32(&mut self, v: f32) { self.u32(v.to_bits()); }
    fn str(&mut self, s: &str) {
        let units: Vec<u16> = s.encode_utf16().collect();
        self.u32(units.len() as u32);
        for u in units {
            self.0.extend_from_slice(&u.to_le_bytes());
        }
    }
}

impl Msg {
    pub fn kind(&self) -> u8 {
        match self {
            Msg::Hello { .. } => kind::HELLO,
            Msg::HookAdded { .. } => kind::HOOK_ADDED,
            Msg::HookRemoved { .. } => kind::HOOK_REMOVED,
            Msg::Text { .. } => kind::TEXT,
            Msg::EmbedRequest { .. } => kind::EMBED_REQUEST,
            Msg::Log { .. } => kind::LOG,
            Msg::Settings(_) => kind::SETTINGS,
            Msg::SetEmbed { .. } => kind::SET_EMBED,
            Msg::EmbedResponse { .. } => kind::EMBED_RESPONSE,
            Msg::Detach => kind::DETACH,
        }
    }

    /// Encodes the full frame (length prefix included).
    pub fn encode(&self) -> Vec<u8> {
        let mut e = Enc(vec![0, 0, 0, 0]);
        e.u8(self.kind());
        match self {
            Msg::Hello { proto, pid, arch, engine, unity_version } => {
                e.u32(*proto);
                e.u32(*pid);
                e.u8(*arch);
                e.str(engine);
                e.str(unity_version);
            }
            Msg::HookAdded { hook_id, code, name, embeddable } => {
                e.u64(*hook_id);
                e.str(code);
                e.str(name);
                e.bool(*embeddable);
            }
            Msg::HookRemoved { hook_id } => e.u64(*hook_id),
            Msg::Text { hook_id, ctx, ctx2, text } => {
                e.u64(*hook_id);
                e.u64(*ctx);
                e.u64(*ctx2);
                e.str(text);
            }
            Msg::EmbedRequest { req_id, hook_id, ctx, ctx2, text } => {
                e.u64(*req_id);
                e.u64(*hook_id);
                e.u64(*ctx);
                e.u64(*ctx2);
                e.str(text);
            }
            Msg::Log { level, msg } => {
                e.u8(*level);
                e.str(msg);
            }
            Msg::Settings(s) => {
                e.u32(s.wait_ms);
                e.u32(s.display_mode);
                e.str(&s.font_family);
                e.str(&s.font_dir);
                e.bool(s.clear_text);
                e.bool(s.change_font_size);
                e.f32(s.font_scale);
                e.bool(s.strip_typewriter_tags);
                e.bool(s.non_blocking);
                e.str(&s.pending_text);
            }
            Msg::SetEmbed { hook_id, ctx, ctx2, on } => {
                e.u64(*hook_id);
                e.u64(*ctx);
                e.u64(*ctx2);
                e.bool(*on);
            }
            Msg::EmbedResponse { req_id, text } => {
                e.u64(*req_id);
                match text {
                    Some(t) => {
                        e.bool(true);
                        e.str(t);
                    }
                    None => e.bool(false),
                }
            }
            Msg::Detach => {}
        }
        let len = (e.0.len() - 4) as u32;
        e.0[..4].copy_from_slice(&len.to_le_bytes());
        e.0
    }

    /// Decodes one frame body (type byte + payload, without the length prefix).
    pub fn decode(body: &[u8]) -> io::Result<Msg> {
        let mut d = Dec { buf: body, pos: 0 };
        let k = d.u8()?;
        let msg = match k {
            kind::HELLO => Msg::Hello {
                proto: d.u32()?,
                pid: d.u32()?,
                arch: d.u8()?,
                engine: d.str()?,
                unity_version: d.str()?,
            },
            kind::HOOK_ADDED => Msg::HookAdded {
                hook_id: d.u64()?,
                code: d.str()?,
                name: d.str()?,
                embeddable: d.bool()?,
            },
            kind::HOOK_REMOVED => Msg::HookRemoved { hook_id: d.u64()? },
            kind::TEXT => Msg::Text { hook_id: d.u64()?, ctx: d.u64()?, ctx2: d.u64()?, text: d.str()? },
            kind::EMBED_REQUEST => Msg::EmbedRequest {
                req_id: d.u64()?,
                hook_id: d.u64()?,
                ctx: d.u64()?,
                ctx2: d.u64()?,
                text: d.str()?,
            },
            kind::LOG => Msg::Log { level: d.u8()?, msg: d.str()? },
            kind::SETTINGS => Msg::Settings(Settings {
                wait_ms: d.u32()?,
                display_mode: d.u32()?,
                font_family: d.str()?,
                font_dir: d.str()?,
                clear_text: d.bool()?,
                change_font_size: d.bool()?,
                font_scale: f32::from_bits(d.u32()?),
                strip_typewriter_tags: d.bool()?,
                non_blocking: d.bool()?,
                pending_text: d.str()?,
            }),
            kind::SET_EMBED => Msg::SetEmbed { hook_id: d.u64()?, ctx: d.u64()?, ctx2: d.u64()?, on: d.bool()? },
            kind::EMBED_RESPONSE => {
                let req_id = d.u64()?;
                let text = if d.bool()? { Some(d.str()?) } else { None };
                Msg::EmbedResponse { req_id, text }
            }
            kind::DETACH => Msg::Detach,
            other => return Err(invalid(format!("unknown message type 0x{other:02x}"))),
        };
        if d.pos != d.buf.len() {
            return Err(invalid(format!("trailing {} bytes in frame", d.buf.len() - d.pos)));
        }
        Ok(msg)
    }
}

struct Dec<'a> {
    buf: &'a [u8],
    pos: usize,
}

impl Dec<'_> {
    fn take(&mut self, n: usize) -> io::Result<&[u8]> {
        if self.buf.len() - self.pos < n {
            return Err(invalid("truncated frame".into()));
        }
        let s = &self.buf[self.pos..self.pos + n];
        self.pos += n;
        Ok(s)
    }
    fn u8(&mut self) -> io::Result<u8> { Ok(self.take(1)?[0]) }
    fn bool(&mut self) -> io::Result<bool> { Ok(self.u8()? != 0) }
    fn u32(&mut self) -> io::Result<u32> { Ok(u32::from_le_bytes(self.take(4)?.try_into().unwrap())) }
    fn u64(&mut self) -> io::Result<u64> { Ok(u64::from_le_bytes(self.take(8)?.try_into().unwrap())) }
    fn str(&mut self) -> io::Result<String> {
        let n = self.u32()? as usize;
        let raw = self.take(n.checked_mul(2).ok_or_else(|| invalid("string too long".into()))?)?;
        let units: Vec<u16> = raw.as_chunks::<2>().0.iter().map(|&c| u16::from_le_bytes(c)).collect();
        Ok(String::from_utf16_lossy(&units))
    }
}

fn invalid(msg: String) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, msg)
}

/// Writes one framed message.
pub fn write_msg<W: Write>(w: &mut W, msg: &Msg) -> io::Result<()> {
    w.write_all(&msg.encode())?;
    w.flush()
}

/// Reads one framed message (blocking).
pub fn read_msg<R: Read>(r: &mut R) -> io::Result<Msg> {
    let mut len = [0u8; 4];
    r.read_exact(&mut len)?;
    let len = u32::from_le_bytes(len);
    if len == 0 || len > MAX_FRAME {
        return Err(invalid(format!("bad frame length {len}")));
    }
    let mut body = vec![0u8; len as usize];
    r.read_exact(&mut body)?;
    Msg::decode(&body)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hex(b: &[u8]) -> String {
        b.iter().map(|x| format!("{x:02x}")).collect()
    }

    fn samples() -> Vec<Msg> {
        vec![
            Msg::Hello { proto: 1, pid: 1234, arch: 64, engine: "unity-mono".into(), unity_version: "2021.3".into() },
            Msg::HookAdded { hook_id: 7, code: "UNITY:TMP_Text.set_text".into(), name: "TMP".into(), embeddable: true },
            Msg::HookRemoved { hook_id: 7 },
            Msg::Text { hook_id: 7, ctx: 1, ctx2: 2, text: "Hello".into() },
            Msg::EmbedRequest { req_id: 9, hook_id: 7, ctx: 0, ctx2: 0, text: "مرحبا 😀".into() },
            Msg::Log { level: level::WARN, msg: "x".into() },
            Msg::Settings(Settings {
                wait_ms: 1000,
                display_mode: 1,
                font_family: "Arial".into(),
                font_dir: "C:\\f".into(),
                clear_text: true,
                change_font_size: false,
                font_scale: 1.5,
                strip_typewriter_tags: true,
                non_blocking: true,
                pending_text: "...".into(),
            }),
            Msg::SetEmbed { hook_id: 7, ctx: 0, ctx2: 0, on: true },
            Msg::EmbedResponse { req_id: 9, text: Some("ok".into()) },
            Msg::EmbedResponse { req_id: 9, text: None },
            Msg::Detach,
        ]
    }

    #[test]
    fn round_trip() {
        for m in samples() {
            let mut buf = Vec::new();
            write_msg(&mut buf, &m).unwrap();
            let got = read_msg(&mut buf.as_slice()).unwrap();
            assert_eq!(got, m);
        }
    }

    // Golden bytes shared with internal/hook/proto_test.go.
    #[test]
    fn golden() {
        let hello = Msg::Hello { proto: 1, pid: 1234, arch: 64, engine: "ab".into(), unity_version: "".into() };
        assert_eq!(hex(&hello.encode()), "160000000101000000d204000040020000006100620000000000");
        let resp = Msg::EmbedResponse { req_id: 9, text: Some("ع".into()) };
        assert_eq!(hex(&resp.encode()), "1000000083090000000000000001010000003906");
    }

    #[test]
    fn rejects_truncated() {
        assert!(Msg::decode(&[kind::HOOK_REMOVED, 1, 2]).is_err());
        assert!(Msg::decode(&[0x55]).is_err());
    }
}
