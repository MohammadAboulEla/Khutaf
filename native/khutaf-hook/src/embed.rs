//! Blocking embed round-trip used by hook detours on the game thread.

use std::collections::{HashMap, HashSet, VecDeque};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Condvar, LazyLock, Mutex, RwLock};
use std::time::{Duration, Instant};

use khutaf_proto::{Msg, Settings};

use crate::ipc;

const DEFAULT_WAIT_MS: u32 = 1000;
/// Translations we produced recently; if the game feeds one back (e.g. reads
/// `.text` and sets it again) it must pass through untouched.
const OWN_OUTPUT_HISTORY: usize = 256;
const MAX_COMPONENT_CTX: usize = 4096;
const MAX_RECENT_TRANS: usize = 512;

static SETTINGS: LazyLock<RwLock<Settings>> =
    LazyLock::new(|| RwLock::new(Settings { wait_ms: DEFAULT_WAIT_MS, font_scale: 1.0, ..Default::default() }));
static ENABLED: LazyLock<Mutex<HashSet<(u64, u64, u64)>>> = LazyLock::new(Default::default);
static COMPONENT_CTX: LazyLock<Mutex<HashMap<(u64, usize), u64>>> = LazyLock::new(Default::default);
static RECENT_TRANSLATIONS: LazyLock<Mutex<HashMap<String, String>>> = LazyLock::new(Default::default);
static OWN_OUTPUTS: LazyLock<Mutex<VecDeque<String>>> = LazyLock::new(Default::default);

/// req_id -> None while waiting, Some(reply) once answered.
static PENDING: LazyLock<Mutex<HashMap<u64, Option<Option<String>>>>> = LazyLock::new(Default::default);
static PENDING_CV: Condvar = Condvar::new();
static NEXT_REQ: AtomicU64 = AtomicU64::new(1);
static SHUTDOWN: AtomicBool = AtomicBool::new(false);

pub fn set_settings(s: Settings) {
    *SETTINGS.write().unwrap_or_else(|p| p.into_inner()) = s;
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
}

pub fn settings() -> Settings {
    SETTINGS.read().unwrap_or_else(|p| p.into_inner()).clone()
}

pub fn set_enabled(hook_id: u64, ctx: u64, ctx2: u64, on: bool) {
    let mut set = ENABLED.lock().unwrap_or_else(|p| p.into_inner());
    if on {
        set.insert((hook_id, ctx, ctx2));
    } else {
        set.remove(&(hook_id, ctx, ctx2));
        drop(set);
        let mut comp = COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner());
        comp.retain(|&(hid, _), &mut mapped_ctx| !(hid == hook_id && (ctx == 0 || mapped_ctx == ctx)));
    }
}

pub fn is_enabled(hook_id: u64, ctx: u64, ctx2: u64) -> bool {
    if SHUTDOWN.load(Ordering::Relaxed) {
        return false;
    }
    let set = ENABLED.lock().unwrap_or_else(|p| p.into_inner());
    set.contains(&(hook_id, ctx, ctx2)) || set.contains(&(hook_id, 0, 0))
}

fn is_own_output(text: &str) -> bool {
    OWN_OUTPUTS.lock().unwrap_or_else(|p| p.into_inner()).iter().any(|t| t == text)
}

fn remember_output(text: &str) {
    let mut q = OWN_OUTPUTS.lock().unwrap_or_else(|p| p.into_inner());
    if q.len() >= OWN_OUTPUT_HISTORY {
        q.pop_front();
    }
    q.push_back(text.to_owned());
}

fn resolve_component_ctx(hook_id: u64, raw_ctx: u64, ctx2: u64, component: usize) -> u64 {
    if component == 0 {
        return raw_ctx;
    }
    let mut map = COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner());
    if let Some(&existing) = map.get(&(hook_id, component)) {
        if existing != raw_ctx && is_enabled(hook_id, raw_ctx, ctx2) && !is_enabled(hook_id, existing, ctx2) {
            map.insert((hook_id, component), raw_ctx);
            return raw_ctx;
        }
        return existing;
    }
    if map.len() >= MAX_COMPONENT_CTX {
        map.clear();
    }
    map.insert((hook_id, component), raw_ctx);
    raw_ctx
}

/// Reports hooked text to the host and, when embedding is enabled for the
/// thread, blocks up to `wait_ms` for a replacement. `None` = keep original.
pub fn process(hook_id: u64, raw_ctx: u64, ctx2: u64, component: usize, text: &str) -> Option<String> {
    if text.trim().is_empty() || is_own_output(text) {
        return None;
    }
    if text.chars().count() <= 1 {
        ipc::send(&Msg::Text { hook_id, ctx: raw_ctx, ctx2, text: text.to_owned() });
        return None;
    }

    let ctx = resolve_component_ctx(hook_id, raw_ctx, ctx2, component);
    if !is_enabled(hook_id, ctx, ctx2) {
        ipc::send(&Msg::Text { hook_id, ctx, ctx2, text: text.to_owned() });
        let cached = RECENT_TRANSLATIONS
            .lock()
            .unwrap_or_else(|p| p.into_inner())
            .get(text)
            .cloned();
        return cached;
    }

    let wait = Duration::from_millis(SETTINGS.read().unwrap_or_else(|p| p.into_inner()).wait_ms.max(1) as u64);
    let req_id = NEXT_REQ.fetch_add(1, Ordering::Relaxed);
    PENDING.lock().unwrap_or_else(|p| p.into_inner()).insert(req_id, None);
    if !ipc::send(&Msg::EmbedRequest { req_id, hook_id, ctx, ctx2, text: text.to_owned() }) {
        PENDING.lock().unwrap_or_else(|p| p.into_inner()).remove(&req_id);
        return None;
    }

    let deadline = Instant::now() + wait;
    let mut pending = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    let reply = loop {
        if let Some(Some(_)) = pending.get(&req_id) {
            break pending.remove(&req_id).flatten().flatten();
        }
        let now = Instant::now();
        if now >= deadline || SHUTDOWN.load(Ordering::Relaxed) {
            pending.remove(&req_id);
            break None;
        }
        pending = PENDING_CV.wait_timeout(pending, deadline - now).unwrap_or_else(|p| p.into_inner()).0;
    };
    drop(pending);

    let reply = reply.filter(|t| !t.is_empty() && t != text)?;
    {
        let mut recent = RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner());
        if recent.len() >= MAX_RECENT_TRANS {
            recent.clear();
        }
        recent.insert(text.to_owned(), reply.clone());
    }
    remember_output(&reply);
    Some(reply)
}

pub fn complete(req_id: u64, text: Option<String>) {
    let mut pending = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    if let Some(slot) = pending.get_mut(&req_id) {
        *slot = Some(text);
        PENDING_CV.notify_all();
    }
}

/// Wakes every waiting game thread so unload never blocks the game.
pub fn release_all() {
    SHUTDOWN.store(true, Ordering::SeqCst);
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
    let _guard = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    PENDING_CV.notify_all();
}

pub fn is_shutting_down() -> bool {
    SHUTDOWN.load(Ordering::Relaxed)
}
