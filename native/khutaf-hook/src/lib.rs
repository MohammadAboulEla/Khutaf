//! khutaf-hook: injected into the game process. Connects back to the Khutaf
//! host over named pipes, installs engine text hooks and performs embedded
//! (in-place) translation.

mod embed;
mod hooks;
mod ipc;
mod unity;

use std::ffi::c_void;
use std::sync::atomic::{AtomicPtr, Ordering};

use khutaf_proto::{level, Msg, PROTO_VERSION};
use windows_sys::Win32::Foundation::{CloseHandle, HMODULE, TRUE};
use windows_sys::Win32::System::LibraryLoader::DisableThreadLibraryCalls;
use windows_sys::Win32::System::SystemServices::DLL_PROCESS_ATTACH;
use windows_sys::Win32::System::Threading::{CreateThread, GetCurrentProcessId};

static MODULE: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());

pub(crate) fn module_base() -> usize {
    MODULE.load(Ordering::Relaxed) as usize
}

/// DLL entry point invoked by the Windows loader.
///
/// # Safety
/// Must only be called by the Windows loader under standard `DllMain` guarantees
/// with a valid module handle and reason code.
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
    loop {
        ipc::connect_wait(pid);
        embed::reset_for_session();
        unity::reset_for_session();
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
        std::thread::sleep(std::time::Duration::from_millis(250));
    }
}

/// Sends a log line to the host (dropped when disconnected).
pub(crate) fn log(lvl: u8, msg: impl Into<String>) {
    ipc::send(&Msg::Log { level: lvl, msg: msg.into() });
}

/// Sends a structured telemetry line over `Msg::Log` without breaking wire compatibility.
pub(crate) fn telemetry(subsys: &str, func: &str, dur_us: u64, lvl: &str, msg: &str) {
    let wire_lvl = match lvl {
        "error" => level::ERROR,
        "warn" => level::WARN,
        _ => level::INFO,
    };
    let line = format!("[telemetry|{subsys}|{func}|{dur_us}|{lvl}] {msg}");
    ipc::send(&Msg::Log { level: wire_lvl, msg: line });
}

/// Sends Unity component hierarchy metadata over `Msg::Log` without breaking wire compatibility.
pub(crate) fn hook_meta(hook_id: u64, ctx: u64, ctx2: u64, path: &str) {
    let line = format!("[meta|{hook_id}|{ctx:x}|{ctx2:x}] {path}");
    ipc::send(&Msg::Log { level: level::INFO, msg: line });
}
