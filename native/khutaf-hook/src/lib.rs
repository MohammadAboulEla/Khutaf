//! khutaf-hook: injected into the game process. Connects back to the Khutaf
//! host over named pipes, installs engine text hooks and performs embedded
//! (in-place) translation.

mod embed;
mod hooks;
mod ipc;
mod unity;

use std::ffi::c_void;
use std::sync::atomic::{AtomicPtr, Ordering};

use khutaf_proto::{Msg, PROTO_VERSION};
use windows_sys::Win32::Foundation::{CloseHandle, HMODULE, TRUE};
use windows_sys::Win32::System::LibraryLoader::DisableThreadLibraryCalls;
use windows_sys::Win32::System::SystemServices::DLL_PROCESS_ATTACH;
use windows_sys::Win32::System::Threading::{CreateThread, GetCurrentProcessId};

static MODULE: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());

pub(crate) fn module_base() -> usize {
    MODULE.load(Ordering::Relaxed) as usize
}

#[no_mangle]
pub unsafe extern "system" fn DllMain(module: HMODULE, reason: u32, _reserved: *mut c_void) -> i32 {
    if reason == DLL_PROCESS_ATTACH {
        DisableThreadLibraryCalls(module);
        MODULE.store(module, Ordering::SeqCst);
        // Never do real work under the loader lock: hand off to a thread.
        let thread = CreateThread(std::ptr::null(), 0, Some(init_thread), std::ptr::null(), 0, std::ptr::null_mut());
        if !thread.is_null() {
            CloseHandle(thread);
        }
    }
    TRUE
}

unsafe extern "system" fn init_thread(_: *mut c_void) -> u32 {
    let _ = std::panic::catch_unwind(run);
    0
}

fn run() {
    let pid = unsafe { GetCurrentProcessId() };
    if ipc::connect(pid).is_err() {
        return; // host is gone; unload silently
    }
    let engine = unity::detect();
    ipc::send(&Msg::Hello {
        proto: PROTO_VERSION,
        pid,
        arch: (std::mem::size_of::<usize>() * 8) as u8,
        engine: engine.name().into(),
        unity_version: String::new(),
    });
    let reader = std::thread::spawn(|| {
        ipc::serve(); // blocks until Detach or the pipe breaks
        embed::release_all();
    });

    unity::install(engine);

    let _ = reader.join();
    hooks::shutdown();
    ipc::close();
}

/// Sends a log line to the host (dropped when disconnected).
pub(crate) fn log(lvl: u8, msg: impl Into<String>) {
    ipc::send(&Msg::Log { level: lvl, msg: msg.into() });
}
