//! Two one-way named pipes created by the host before injection:
//! `\\.\pipe\khutaf_<pid>_<nonce>_up` (DLL -> host) and `..._down` (host -> DLL),
//! where `<nonce>` is discovered from shared memory `Local\khutaf_ipc_<pid>`.
//! Separate handles avoid synchronous-I/O serialization between the blocking
//! reader thread and game threads that send.

use std::fs::{File, OpenOptions};
use std::io::{self, BufReader, Write};
use std::os::windows::io::AsRawHandle;
use std::sync::atomic::{AtomicIsize, Ordering};
use std::sync::mpsc::{self, SyncSender, TrySendError};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use khutaf_proto::{read_msg, Msg};
use windows_sys::Win32::Foundation::CloseHandle;
use windows_sys::Win32::System::IO::CancelIoEx;
use windows_sys::Win32::System::Memory::{MapViewOfFile, OpenFileMappingW, UnmapViewOfFile, FILE_MAP_READ};

use crate::embed;

const SEND_QUEUE_CAP: usize = 512;

static TX: Mutex<Option<SyncSender<Vec<u8>>>> = Mutex::new(None);
static DOWN: Mutex<Option<File>> = Mutex::new(None);
static DOWN_HANDLE: AtomicIsize = AtomicIsize::new(0);

const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const HANDSHAKE_SIZE: usize = 64;
const HANDSHAKE_MAGIC: &[u8; 4] = b"KHUT";
const HANDSHAKE_VERSION: u32 = 1;
const HANDSHAKE_NONCE_LEN: usize = 32;

fn parse_handshake_block(buf: &[u8; HANDSHAKE_SIZE], expected_pid: u32) -> Option<String> {
    if &buf[0..4] != HANDSHAKE_MAGIC {
        return None;
    }
    let version = u32::from_le_bytes(buf[4..8].try_into().ok()?);
    let pid = u32::from_le_bytes(buf[8..12].try_into().ok()?);
    let nonce_len = u32::from_le_bytes(buf[12..16].try_into().ok()?) as usize;
    if version != HANDSHAKE_VERSION || pid != expected_pid || nonce_len != HANDSHAKE_NONCE_LEN {
        return None;
    }
    let nonce = &buf[16..48];
    if !nonce.iter().all(|b| b.is_ascii_hexdigit()) {
        return None;
    }
    std::str::from_utf8(nonce).ok().map(str::to_owned)
}

fn read_session_nonce(pid: u32) -> Option<String> {
    let mut name: Vec<u16> = format!(r"Local\khutaf_ipc_{pid}").encode_utf16().collect();
    name.push(0);
    // Safety: name is a valid NUL-terminated UTF-16 string and view points to HANDSHAKE_SIZE bytes when non-null.
    let handle = unsafe { OpenFileMappingW(FILE_MAP_READ, 0, name.as_ptr()) };
    if handle.is_null() {
        return None;
    }
    let view = unsafe { MapViewOfFile(handle, FILE_MAP_READ, 0, 0, HANDSHAKE_SIZE) };
    if view.Value.is_null() {
        unsafe { CloseHandle(handle) };
        return None;
    }
    let mut buf = [0u8; HANDSHAKE_SIZE];
    unsafe {
        std::ptr::copy_nonoverlapping(view.Value.cast::<u8>(), buf.as_mut_ptr(), HANDSHAKE_SIZE);
        UnmapViewOfFile(view);
        CloseHandle(handle);
    }
    parse_handshake_block(&buf, pid)
}

fn open_retry(path: &str, write: bool) -> io::Result<File> {
    let deadline = Instant::now() + CONNECT_TIMEOUT;
    loop {
        match OpenOptions::new().read(!write).write(write).open(path) {
            Ok(f) => return Ok(f),
            Err(e) if Instant::now() >= deadline => return Err(e),
            Err(_) => std::thread::sleep(Duration::from_millis(50)),
        }
    }
}

pub fn connect_wait(pid: u32) {
    let mut backoff = Duration::from_millis(250);
    loop {
        let Some(nonce) = read_session_nonce(pid) else {
            std::thread::sleep(backoff);
            backoff = (backoff * 2).min(Duration::from_millis(2000));
            continue;
        };
        let up_path = format!(r"\\.\pipe\khutaf_{pid}_{nonce}_up");
        let down_path = format!(r"\\.\pipe\khutaf_{pid}_{nonce}_down");
        let Ok(mut up) = OpenOptions::new().write(true).open(&up_path) else {
            std::thread::sleep(backoff);
            backoff = (backoff * 2).min(Duration::from_millis(2000));
            continue;
        };
        backoff = Duration::from_millis(250);
        let Ok(down) = open_retry(&down_path, false) else {
            drop(up);
            std::thread::sleep(Duration::from_millis(250));
            continue;
        };
        let (tx, rx) = mpsc::sync_channel::<Vec<u8>>(SEND_QUEUE_CAP);
        std::thread::spawn(move || {
            while let Ok(frame) = rx.recv() {
                if up.write_all(&frame).is_err() {
                    break;
                }
            }
            *TX.lock().unwrap_or_else(|p| p.into_inner()) = None;
            let h = DOWN_HANDLE.swap(0, Ordering::AcqRel);
            if h != 0 {
                unsafe {
                    CancelIoEx(h as *mut _, std::ptr::null());
                }
            }
        });
        *TX.lock().unwrap_or_else(|p| p.into_inner()) = Some(tx);
        *DOWN.lock().unwrap_or_else(|p| p.into_inner()) = Some(down);
        return;
    }
}

/// Enqueues a message for async pipe write; drops it without blocking when full or disconnected.
pub fn send(msg: &Msg) -> bool {
    let frame = msg.encode();
    let mut guard = match TX.lock() {
        Ok(g) => g,
        Err(p) => p.into_inner(),
    };
    let Some(tx) = guard.as_ref() else {
        return false;
    };
    match tx.try_send(frame) {
        Ok(()) => true,
        Err(TrySendError::Full(_)) => false,
        Err(TrySendError::Disconnected(_)) => {
            *guard = None;
            false
        }
    }
}

struct DownHandleGuard;
impl Drop for DownHandleGuard {
    fn drop(&mut self) {
        DOWN_HANDLE.store(0, Ordering::Release);
    }
}

/// Reads host messages until Detach or a pipe error.
pub fn serve() {
    let Some(down) = DOWN.lock().unwrap_or_else(|p| p.into_inner()).take() else { return };
    DOWN_HANDLE.store(down.as_raw_handle() as isize, Ordering::Release);
    let _guard = DownHandleGuard;
    let mut reader = BufReader::new(down);
    loop {
        match read_msg(&mut reader) {
            Ok(Msg::Settings(s)) => embed::set_settings(s),
            Ok(Msg::SetEmbed { hook_id, ctx, ctx2, on }) => embed::set_enabled(hook_id, ctx, ctx2, on),
            Ok(Msg::EmbedResponse { req_id, text }) => embed::complete(req_id, text),
            Ok(Msg::Detach) | Err(_) => break,
            Ok(_) => {} // host never sends DLL->host kinds; ignore
        }
    }
}

pub fn close() {
    DOWN_HANDLE.store(0, Ordering::Release);
    *TX.lock().unwrap_or_else(|p| p.into_inner()) = None;
    *DOWN.lock().unwrap_or_else(|p| p.into_inner()) = None;
}

#[cfg(test)]
pub(crate) fn install_test_sink() -> mpsc::Receiver<Vec<u8>> {
    let (tx, rx) = mpsc::sync_channel(64);
    *TX.lock().unwrap_or_else(|p| p.into_inner()) = Some(tx);
    rx
}

#[cfg(test)]
pub(crate) fn clear_test_sink() {
    *TX.lock().unwrap_or_else(|p| p.into_inner()) = None;
}

#[cfg(test)]
mod tests {
    use super::*;

    fn valid_block(pid: u32) -> [u8; HANDSHAKE_SIZE] {
        let mut buf = [0u8; HANDSHAKE_SIZE];
        buf[0..4].copy_from_slice(HANDSHAKE_MAGIC);
        buf[4..8].copy_from_slice(&HANDSHAKE_VERSION.to_le_bytes());
        buf[8..12].copy_from_slice(&pid.to_le_bytes());
        buf[12..16].copy_from_slice(&(HANDSHAKE_NONCE_LEN as u32).to_le_bytes());
        buf[16..48].copy_from_slice(b"0123456789abcdef0123456789abcdef");
        buf
    }

    #[test]
    fn parse_handshake_validates_fields() {
        let ok = valid_block(4242);
        assert_eq!(parse_handshake_block(&ok, 4242).as_deref(), Some("0123456789abcdef0123456789abcdef"));

        let mut bad_magic = ok;
        bad_magic[0] = b'X';
        assert_eq!(parse_handshake_block(&bad_magic, 4242), None);

        let mut bad_ver = ok;
        bad_ver[4..8].copy_from_slice(&2u32.to_le_bytes());
        assert_eq!(parse_handshake_block(&bad_ver, 4242), None);

        assert_eq!(parse_handshake_block(&ok, 9999), None);

        let mut bad_len = ok;
        bad_len[12..16].copy_from_slice(&16u32.to_le_bytes());
        assert_eq!(parse_handshake_block(&bad_len, 4242), None);

        let mut bad_hex = ok;
        bad_hex[20] = b'z';
        assert_eq!(parse_handshake_block(&bad_hex, 4242), None);
    }
}
