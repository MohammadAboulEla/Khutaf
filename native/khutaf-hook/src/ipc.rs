//! Two one-way named pipes created by the host before injection:
//! `\\.\pipe\khutaf_<pid>_up` (DLL -> host) and `..._down` (host -> DLL).
//! Separate handles avoid synchronous-I/O serialization between the blocking
//! reader thread and game threads that send.

use std::fs::{File, OpenOptions};
use std::io::{self, BufReader};
use std::sync::Mutex;
use std::time::{Duration, Instant};

use khutaf_proto::{read_msg, write_msg, Msg};

use crate::embed;

static UP: Mutex<Option<File>> = Mutex::new(None);
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

pub fn connect(pid: u32) -> io::Result<()> {
    let up = open_retry(&format!(r"\\.\pipe\khutaf_{pid}_up"), true)?;
    let down = open_retry(&format!(r"\\.\pipe\khutaf_{pid}_down"), false)?;
    *UP.lock().unwrap() = Some(up);
    *DOWN.lock().unwrap() = Some(down);
    Ok(())
}

/// Sends a message; silently drops it when disconnected or the pipe broke.
pub fn send(msg: &Msg) -> bool {
    let mut guard = match UP.lock() {
        Ok(g) => g,
        Err(p) => p.into_inner(),
    };
    match guard.as_mut() {
        Some(f) => {
            if write_msg(f, msg).is_err() {
                *guard = None;
                return false;
            }
            true
        }
        None => false,
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
    *UP.lock().unwrap_or_else(|p| p.into_inner()) = None;
    *DOWN.lock().unwrap_or_else(|p| p.into_inner()) = None;
}
