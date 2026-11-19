//! Two one-way named pipes created by the host before injection:
//! `\\.\pipe\khutaf_<pid>_up` (DLL -> host) and `..._down` (host -> DLL).
//! Separate handles avoid synchronous-I/O serialization between the blocking
//! reader thread and game threads that send.

use std::fs::{File, OpenOptions};
use std::io::{self, BufReader, Write};
use std::sync::mpsc::{self, SyncSender, TrySendError};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use khutaf_proto::{read_msg, Msg};

use crate::embed;

const SEND_QUEUE_CAP: usize = 512;

static TX: Mutex<Option<SyncSender<Vec<u8>>>> = Mutex::new(None);
static DOWN: Mutex<Option<File>> = Mutex::new(None);

const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

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
    let up_path = format!(r"\\.\pipe\khutaf_{pid}_up");
    let down_path = format!(r"\\.\pipe\khutaf_{pid}_down");
    loop {
        let Ok(mut up) = OpenOptions::new().write(true).open(&up_path) else {
            std::thread::sleep(Duration::from_millis(250));
            continue;
        };
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

/// Reads host messages until Detach or a pipe error.
pub fn serve() {
    let Some(down) = DOWN.lock().unwrap().take() else { return };
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
    *TX.lock().unwrap_or_else(|p| p.into_inner()) = None;
    *DOWN.lock().unwrap_or_else(|p| p.into_inner()) = None;
}
