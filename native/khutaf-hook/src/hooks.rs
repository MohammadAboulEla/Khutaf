//! Thin wrapper over MinHook with a per-thread reentrancy guard and an
//! in-flight counter so the DLL never unloads while a detour is running.

use std::cell::Cell;
use std::ffi::c_void;
use std::sync::atomic::{AtomicUsize, Ordering};
use std::time::{Duration, Instant};

use minhook::MinHook;

static IN_FLIGHT: AtomicUsize = AtomicUsize::new(0);

thread_local! {
    static DEPTH: Cell<u32> = const { Cell::new(0) };
}

/// Creates (but does not enable) a hook; returns the trampoline to call the original.
#[allow(dead_code)] // used by engine modules (Phase 3+)
pub unsafe fn create(target: *mut c_void, detour: *mut c_void) -> Result<*mut c_void, String> {
    MinHook::create_hook(target, detour).map_err(|s| format!("{s:?}"))
}

#[allow(dead_code)]
pub unsafe fn enable_all() -> Result<(), String> {
    MinHook::enable_all_hooks().map_err(|s| format!("{s:?}"))
}

/// Disables every hook and waits (bounded) for running detours to return.
pub fn shutdown() {
    let start = Instant::now();
    let initial_in_flight = IN_FLIGHT.load(Ordering::SeqCst);
    unsafe {
        let _ = MinHook::disable_all_hooks();
    }
    let deadline = Instant::now() + Duration::from_secs(3);
    while IN_FLIGHT.load(Ordering::SeqCst) > 0 && Instant::now() < deadline {
        std::thread::sleep(Duration::from_millis(10));
    }
    let remaining = IN_FLIGHT.load(Ordering::SeqCst);
    let dur_us = start.elapsed().as_micros() as u64;
    if remaining > 0 {
        crate::telemetry(
            "HOOK_NATIVE",
            "hooks::shutdown",
            dur_us,
            "error",
            &format!("drain deadline expired | initial={initial_in_flight} remaining={remaining}"),
        );
    } else if initial_in_flight > 0 {
        crate::telemetry(
            "HOOK_NATIVE",
            "hooks::shutdown",
            dur_us,
            "info",
            &format!("drained detours | initial={initial_in_flight}"),
        );
    }
    // Trampolines may still be referenced by a thread that just left a detour,
    // so MinHook is intentionally not uninitialized (its buffers are leaked).
    std::thread::sleep(Duration::from_millis(50));
}

/// RAII guard held for the duration of a detour body. `None` when the
/// current thread is already inside a detour (call the original directly).
#[allow(dead_code)]
pub struct Enter(());

#[allow(dead_code)]
impl Enter {
    pub fn try_new() -> Option<Enter> {
        let nested = DEPTH.with(|d| {
            let v = d.get();
            d.set(v + 1);
            v > 0
        });
        IN_FLIGHT.fetch_add(1, Ordering::SeqCst);
        let guard = Enter(());
        if nested {
            None // dropping `guard` here restores the counters
        } else {
            Some(guard)
        }
    }
}

impl Drop for Enter {
    fn drop(&mut self) {
        DEPTH.with(|d| d.set(d.get().saturating_sub(1)));
        IN_FLIGHT.fetch_sub(1, Ordering::SeqCst);
    }
}
