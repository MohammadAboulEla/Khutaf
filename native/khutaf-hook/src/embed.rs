//! Blocking embed round-trip used by hook detours on the game thread.

use std::collections::{HashMap, HashSet, VecDeque};
use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU64, Ordering};
use std::sync::{Arc, Condvar, LazyLock, Mutex, RwLock};
use std::time::{Duration, Instant};

use khutaf_proto::{Msg, Settings};

use crate::ipc;

const DEFAULT_WAIT_MS: u32 = 1000;
/// Translations we produced recently; if the game feeds one back (e.g. reads
/// `.text` and sets it again) it must pass through untouched.
const OWN_OUTPUT_HISTORY: usize = 256;
const MAX_COMPONENT_CTX: usize = 4096;
const MAX_DISABLED_REPROBES: u8 = 2;
const MAX_RECENT_TRANS: usize = 4096;
const MAX_LAST_SENT: usize = 512;
const TEXT_RATE_LIMIT: Duration = Duration::from_millis(250);
const ERROR_CACHE_COOLDOWN: Duration = Duration::from_secs(5);
const TIMEOUT_CACHE_COOLDOWN: Duration = Duration::from_secs(4);
const CB_THRESHOLD: u32 = 3;
const CB_OPEN_DURATION: Duration = Duration::from_secs(8);
const TELEMETRY_RATE_LIMIT: Duration = Duration::from_secs(1);
const MAX_LATE_PENDING: usize = 32;
const MAX_ASYNC_IN_FLIGHT: usize = 64;
const ASYNC_TTL: Duration = Duration::from_secs(16);

#[derive(Debug, PartialEq, Eq)]
pub enum ProcessOutcome {
    KeepOriginal,
    Replace { text: String, apply_font: bool },
}

pub struct ReadyUpdate {
    pub hook_id: u64,
    pub component: usize,
    pub gc_handle: u32,
    pub text: String,
    pub apply_font: bool,
}

struct AsyncTarget {
    req_id: u64,
    hook_id: u64,
    component: usize,
    gc_handle: u32,
    generation: u64,
    orig_text: String,
    raw_orig_text: String,
    pending_text: String,
    display_mode: u32,
    showed_placeholder: bool,
    created_at: Instant,
}

#[derive(Default)]
struct AsyncState {
    next_gen: u64,
    comp_gen: HashMap<usize, u64>,
    in_flight: HashMap<u64, AsyncTarget>,
    ready: VecDeque<ReadyUpdate>,
    handles_to_free: Vec<u32>,
}

#[derive(Default)]
struct CircuitState {
    consecutive_timeouts: u32,
    open_until: Option<Instant>,
    last_telem_at: Option<Instant>,
    suppressed_telem: u32,
}

#[derive(Default)]
struct OwnOutputs {
    queue: VecDeque<String>,
    set: HashSet<String>,
}

struct RecentEntry {
    trans: Option<String>,
    expires_at: Option<Instant>,
}

#[derive(Default)]
struct RecentCache {
    map: HashMap<String, RecentEntry>,
    order: VecDeque<String>,
}

impl RecentCache {
    fn get(&mut self, text: &str) -> Option<Option<String>> {
        let entry = self.map.get(text)?;
        if let Some(exp) = entry.expires_at {
            if Instant::now() >= exp {
                self.map.remove(text);
                return None;
            }
        }
        Some(entry.trans.clone())
    }

    fn insert(&mut self, text: String, trans: Option<String>, expires_at: Option<Instant>) {
        if let Some(existing) = self.map.get_mut(&text) {
            existing.trans = trans;
            existing.expires_at = expires_at;
            return;
        }
        while self.order.len() >= MAX_RECENT_TRANS {
            if let Some(old) = self.order.pop_front() {
                self.map.remove(&old);
            }
        }
        self.order.push_back(text.clone());
        self.map.insert(text, RecentEntry { trans, expires_at });
    }

    fn clear(&mut self) {
        self.map.clear();
        self.order.clear();
    }
}

#[derive(Clone, Copy)]
struct ComponentEntry {
    ctx: u64,
    enabled_gen: u64,
    enabled: bool,
    last_text_hash: u64,
    reprobe_count: u8,
}

static SETTINGS: LazyLock<RwLock<Arc<Settings>>> = LazyLock::new(|| {
    RwLock::new(Arc::new(Settings { wait_ms: DEFAULT_WAIT_MS, font_scale: 1.0, ..Default::default() }))
});
static ENABLED: LazyLock<Mutex<HashSet<(u64, u64, u64)>>> = LazyLock::new(Default::default);
static ENABLED_GEN: AtomicU64 = AtomicU64::new(1);
static ENABLED_HOOK_MASK: AtomicU64 = AtomicU64::new(0);
static ENABLED_HIGH_HOOKS: AtomicBool = AtomicBool::new(false);
static COMPONENT_CTX: LazyLock<Mutex<HashMap<(u64, usize), ComponentEntry>>> = LazyLock::new(Default::default);
static RECENT_TRANSLATIONS: LazyLock<Mutex<RecentCache>> = LazyLock::new(Default::default);
static OWN_OUTPUTS: LazyLock<Mutex<OwnOutputs>> = LazyLock::new(Default::default);
static HAS_OWN_OUTPUTS: AtomicBool = AtomicBool::new(false);
static DISPLAY_MODE: AtomicU32 = AtomicU32::new(0);
static STRIP_TYPEWRITER: AtomicBool = AtomicBool::new(false);
static CLEAR_TEXT: AtomicBool = AtomicBool::new(false);
static NON_BLOCKING: AtomicBool = AtomicBool::new(false);
static FRAME_TICK_ACTIVE: AtomicBool = AtomicBool::new(false);
static HAS_ASYNC_WORK: AtomicBool = AtomicBool::new(false);
static HAS_ASYNC_IN_FLIGHT: AtomicBool = AtomicBool::new(false);
static ASYNC: LazyLock<Mutex<AsyncState>> = LazyLock::new(Default::default);
type LastSentMap = HashMap<(u64, u64, u64), (String, Instant)>;

static LAST_SENT_TEXT: LazyLock<Mutex<LastSentMap>> = LazyLock::new(Default::default);

/// req_id -> None while waiting, Some(reply) once answered.
static PENDING: LazyLock<Mutex<HashMap<u64, Option<Option<String>>>>> = LazyLock::new(Default::default);
static LATE_PENDING: LazyLock<Mutex<VecDeque<(u64, String)>>> = LazyLock::new(Default::default);
static CIRCUIT: LazyLock<Mutex<CircuitState>> = LazyLock::new(Default::default);
static PENDING_CV: Condvar = Condvar::new();
static NEXT_REQ: AtomicU64 = AtomicU64::new(1);
static SHUTDOWN: AtomicBool = AtomicBool::new(false);

fn sync_enabled_summary(set: &HashSet<(u64, u64, u64)>) {
    let mut mask = 0u64;
    let mut high = false;
    for &(hid, _, _) in set {
        if hid < 64 {
            mask |= 1u64 << hid;
        } else {
            high = true;
        }
    }
    ENABLED_HOOK_MASK.store(mask, Ordering::Release);
    ENABLED_HIGH_HOOKS.store(high, Ordering::Release);
    ENABLED_GEN.fetch_add(1, Ordering::AcqRel);
}

#[inline]
fn has_any_enabled_for_hook(hook_id: u64) -> bool {
    if SHUTDOWN.load(Ordering::Relaxed) {
        return false;
    }
    if hook_id < 64 {
        (ENABLED_HOOK_MASK.load(Ordering::Acquire) & (1u64 << hook_id)) != 0
    } else {
        ENABLED_HIGH_HOOKS.load(Ordering::Acquire)
    }
}

#[inline]
fn fnv1a_64(bytes: &[u8]) -> u64 {
    let mut h = 0xcbf29ce484222325u64;
    for &b in bytes {
        h ^= b as u64;
        h = h.wrapping_mul(0x100000001b3);
    }
    h
}

pub fn set_settings(s: Settings) {
    DISPLAY_MODE.store(s.display_mode, Ordering::Relaxed);
    STRIP_TYPEWRITER.store(s.strip_typewriter_tags, Ordering::Relaxed);
    CLEAR_TEXT.store(s.clear_text, Ordering::Relaxed);
    NON_BLOCKING.store(s.non_blocking, Ordering::Relaxed);
    *SETTINGS.write().unwrap_or_else(|p| p.into_inner()) = Arc::new(s);
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
}

pub fn settings() -> Arc<Settings> {
    Arc::clone(&SETTINGS.read().unwrap_or_else(|p| p.into_inner()))
}

pub(crate) fn strip_typewriter_tags() -> bool {
    STRIP_TYPEWRITER.load(Ordering::Relaxed)
}

pub(crate) fn clear_text_enabled() -> bool {
    CLEAR_TEXT.load(Ordering::Relaxed)
}

pub(crate) fn set_frame_tick_active(active: bool) {
    FRAME_TICK_ACTIVE.store(active, Ordering::Release);
}

pub(crate) fn has_async_work() -> bool {
    HAS_ASYNC_WORK.load(Ordering::Acquire)
}

pub(crate) fn has_async_in_flight() -> bool {
    HAS_ASYNC_IN_FLIGHT.load(Ordering::Acquire)
}

fn sync_async_flags(st: &AsyncState) {
    HAS_ASYNC_WORK.store(!st.ready.is_empty() || !st.handles_to_free.is_empty(), Ordering::Release);
    HAS_ASYNC_IN_FLIGHT.store(!st.in_flight.is_empty(), Ordering::Release);
}

fn push_late_pending(req_id: u64, orig: String) {
    let mut late = LATE_PENDING.lock().unwrap_or_else(|p| p.into_inner());
    if late.len() >= MAX_LATE_PENDING {
        late.pop_front();
    }
    late.push_back((req_id, orig));
}

pub fn set_enabled(hook_id: u64, ctx: u64, ctx2: u64, on: bool) {
    let mut set = ENABLED.lock().unwrap_or_else(|p| p.into_inner());
    if on {
        set.insert((hook_id, ctx, ctx2));
    } else {
        set.remove(&(hook_id, ctx, ctx2));
    }
    sync_enabled_summary(&set);
    drop(set);
    if !on {
        let mut comp = COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner());
        comp.retain(|&(hid, _), entry| !(hid == hook_id && (ctx == 0 || entry.ctx == ctx)));
    }
}

pub fn is_enabled(hook_id: u64, ctx: u64, ctx2: u64) -> bool {
    if !has_any_enabled_for_hook(hook_id) {
        return false;
    }
    let set = ENABLED.lock().unwrap_or_else(|p| p.into_inner());
    set.contains(&(hook_id, ctx, ctx2)) || set.contains(&(hook_id, 0, 0))
}

fn is_own_output(text: &str) -> bool {
    if !HAS_OWN_OUTPUTS.load(Ordering::Acquire) {
        return false;
    }
    OWN_OUTPUTS.lock().unwrap_or_else(|p| p.into_inner()).set.contains(text)
}

pub(crate) fn remember_output(text: &str) {
    let mut o = OWN_OUTPUTS.lock().unwrap_or_else(|p| p.into_inner());
    if o.set.contains(text) {
        return;
    }
    if o.queue.len() >= OWN_OUTPUT_HISTORY {
        if let Some(old) = o.queue.pop_front() {
            o.set.remove(&old);
        }
    }
    let owned = text.to_owned();
    o.set.insert(owned.clone());
    o.queue.push_back(owned);
    HAS_OWN_OUTPUTS.store(true, Ordering::Release);
}

/// Resolves `(ctx, is_enabled, unchanged_disabled)` while avoiding repeated stack walks
/// on UI elements whose embed hook is disabled.
fn resolve_component(
    hook_id: u64,
    ctx_fn: impl FnOnce() -> u64,
    ctx2: u64,
    component: usize,
    text_hash: u64,
) -> (u64, bool, bool) {
    if component == 0 {
        let raw_ctx = ctx_fn();
        return (raw_ctx, is_enabled(hook_id, raw_ctx, ctx2), false);
    }

    let cur_gen = ENABLED_GEN.load(Ordering::Acquire);
    let any_enabled = has_any_enabled_for_hook(hook_id);
    let existing = COMPONENT_CTX
        .lock()
        .unwrap_or_else(|p| p.into_inner())
        .get(&(hook_id, component))
        .copied();

    if let Some(mut entry) = existing {
        if entry.enabled_gen == cur_gen {
            if entry.enabled {
                return (entry.ctx, true, false);
            }
            if entry.last_text_hash == text_hash {
                return (entry.ctx, false, true);
            }
            if !any_enabled || entry.reprobe_count >= MAX_DISABLED_REPROBES {
                if let Some(slot) = COMPONENT_CTX
                    .lock()
                    .unwrap_or_else(|p| p.into_inner())
                    .get_mut(&(hook_id, component))
                {
                    slot.last_text_hash = text_hash;
                }
                return (entry.ctx, false, false);
            }

            let raw_ctx = ctx_fn();
            if raw_ctx != entry.ctx && is_enabled(hook_id, raw_ctx, ctx2) {
                entry.ctx = raw_ctx;
                entry.enabled = true;
                entry.reprobe_count = 0;
            } else {
                entry.reprobe_count = entry.reprobe_count.saturating_add(1);
            }
            entry.last_text_hash = text_hash;
            if let Some(slot) = COMPONENT_CTX
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .get_mut(&(hook_id, component))
            {
                *slot = entry;
            }
            return (entry.ctx, entry.enabled, false);
        }

        let mut now_enabled = any_enabled && is_enabled(hook_id, entry.ctx, ctx2);
        if !now_enabled && any_enabled {
            let raw_ctx = ctx_fn();
            if raw_ctx != entry.ctx && is_enabled(hook_id, raw_ctx, ctx2) {
                entry.ctx = raw_ctx;
                now_enabled = true;
            }
        }
        entry.enabled_gen = cur_gen;
        entry.enabled = now_enabled;
        entry.reprobe_count = 0;
        entry.last_text_hash = text_hash;
        if let Some(slot) = COMPONENT_CTX
            .lock()
            .unwrap_or_else(|p| p.into_inner())
            .get_mut(&(hook_id, component))
        {
            *slot = entry;
        }
        return (entry.ctx, now_enabled, false);
    }

    let raw_ctx = ctx_fn();
    let enabled = any_enabled && is_enabled(hook_id, raw_ctx, ctx2);
    let mut map = COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner());
    if map.len() >= MAX_COMPONENT_CTX {
        map.clear();
    }
    map.insert(
        (hook_id, component),
        ComponentEntry {
            ctx: raw_ctx,
            enabled_gen: cur_gen,
            enabled,
            last_text_hash: text_hash,
            reprobe_count: 0,
        },
    );
    (raw_ctx, enabled, false)
}

fn send_text_dedup(hook_id: u64, ctx: u64, ctx2: u64, text: &str) {
    let now = Instant::now();
    {
        let mut last = LAST_SENT_TEXT.lock().unwrap_or_else(|p| p.into_inner());
        if let Some((prev, prev_at)) = last.get(&(hook_id, ctx, ctx2)) {
            if prev == text || now.duration_since(*prev_at) < TEXT_RATE_LIMIT {
                return;
            }
        }
        if last.len() >= MAX_LAST_SENT && !last.contains_key(&(hook_id, ctx, ctx2)) {
            last.clear();
        }
        last.insert((hook_id, ctx, ctx2), (text.to_owned(), now));
    }
    ipc::send(&Msg::Text { hook_id, ctx, ctx2, text: text.to_owned() });
}

fn is_arabic_char(c: char) -> bool {
    matches!(
        c as u32,
        0x0600..=0x06FF | 0x0750..=0x077F | 0x08A0..=0x08FF | 0xFB50..=0xFDFF | 0xFE70..=0xFEFF
    )
}

/// Reports whether `s` contains at least one non-Arabic alphabetic letter outside `<...>` rich-text tags.
pub(crate) fn has_translatable_letters(s: &str) -> bool {
    let mut in_tag = false;
    let bytes = s.as_bytes();
    for (idx, ch) in s.char_indices() {
        if in_tag {
            if ch == '>' {
                in_tag = false;
            }
            continue;
        }
        if ch == '<' && bytes[idx + 1..].contains(&b'>') {
            in_tag = true;
            continue;
        }
        if ch.is_alphabetic() && !is_arabic_char(ch) {
            return true;
        }
    }
    false
}

fn format_display(mode: u32, orig: &str, trans: &str) -> Option<String> {
    match mode {
        1 => Some(format!("{orig}\n{trans}")),
        2 => Some(format!("{trans}\n{orig}")),
        3 => None,
        _ => Some(trans.to_owned()),
    }
}

/// Reports hooked text to the host and either handles translation asynchronously
/// (when `non_blocking` and frame-tick writeback are active) or blocks up to `wait_ms`.
pub fn process_unity(
    hook_id: u64,
    ctx_fn: impl FnOnce() -> u64,
    ctx2: u64,
    component: usize,
    cleaned: &str,
    raw: &str,
    pin_fn: impl FnOnce() -> u32,
) -> ProcessOutcome {
    if cleaned.trim().is_empty() || is_own_output(cleaned) {
        return ProcessOutcome::KeepOriginal;
    }

    let display_mode = DISPLAY_MODE.load(Ordering::Relaxed);
    let clear_text = CLEAR_TEXT.load(Ordering::Relaxed);
    let (ctx, enabled, unchanged_disabled) =
        resolve_component(hook_id, ctx_fn, ctx2, component, fnv1a_64(cleaned.as_bytes()));
    if unchanged_disabled {
        return ProcessOutcome::KeepOriginal;
    }
    if !enabled || display_mode == 3 || cleaned.chars().count() <= 1 {
        if has_translatable_letters(cleaned) {
            send_text_dedup(hook_id, ctx, ctx2, cleaned);
        }
        return ProcessOutcome::KeepOriginal;
    }
    if clear_text {
        send_text_dedup(hook_id, ctx, ctx2, cleaned);
        return ProcessOutcome::Replace { text: " ".to_owned(), apply_font: false };
    }
    if !has_translatable_letters(cleaned) {
        return ProcessOutcome::KeepOriginal;
    }
    let cfg = settings();
    if let Some(cached) = RECENT_TRANSLATIONS
        .lock()
        .unwrap_or_else(|p| p.into_inner())
        .get(cleaned)
    {
        let Some(cached) = cached else { return ProcessOutcome::KeepOriginal };
        let Some(out) = format_display(cfg.display_mode, cleaned, &cached) else {
            return ProcessOutcome::KeepOriginal;
        };
        remember_output(&out);
        return ProcessOutcome::Replace { text: out, apply_font: true };
    }

    {
        let cb = CIRCUIT.lock().unwrap_or_else(|p| p.into_inner());
        if let Some(until) = cb.open_until {
            if Instant::now() < until {
                return ProcessOutcome::KeepOriginal;
            }
        }
    }

    if NON_BLOCKING.load(Ordering::Relaxed) && FRAME_TICK_ACTIVE.load(Ordering::Acquire) && component != 0 {
        let mut stale_for_late: Option<(u64, String)> = None;
        {
            let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
            let same_req = st
                .in_flight
                .iter()
                .find(|(_, t)| t.hook_id == hook_id && t.component == component)
                .map(|(&rid, _)| rid);
            if let Some(old_rid) = same_req {
                let existing = &st.in_flight[&old_rid];
                if existing.orig_text == cleaned {
                    return if existing.showed_placeholder {
                        ProcessOutcome::Replace {
                            text: existing.pending_text.clone(),
                            apply_font: !existing.pending_text.is_ascii(),
                        }
                    } else {
                        ProcessOutcome::KeepOriginal
                    };
                }
                if existing.showed_placeholder
                    && (cleaned == existing.pending_text || raw == existing.pending_text)
                {
                    return ProcessOutcome::KeepOriginal;
                }
                if let Some(old) = st.in_flight.remove(&old_rid) {
                    if old.gc_handle != 0 {
                        st.handles_to_free.push(old.gc_handle);
                    }
                    stale_for_late = Some((old.req_id, old.orig_text));
                    sync_async_flags(&st);
                }
            }
            if st.in_flight.len() >= MAX_ASYNC_IN_FLIGHT {
                if let Some(&oldest_rid) = st.in_flight.keys().min() {
                    if let Some(evicted) = st.in_flight.remove(&oldest_rid) {
                        if evicted.gc_handle != 0 {
                            st.handles_to_free.push(evicted.gc_handle);
                        }
                        sync_async_flags(&st);
                        // Keep latest evicted line in LATE_PENDING after dropping lock if needed.
                        if stale_for_late.is_none() {
                            stale_for_late = Some((evicted.req_id, evicted.orig_text));
                        }
                    }
                }
            }
        }
        if let Some((old_rid, old_text)) = stale_for_late {
            push_late_pending(old_rid, old_text);
        }

        let gc_handle = pin_fn();
        if gc_handle != 0 {
            let req_id = NEXT_REQ.fetch_add(1, Ordering::Relaxed);
            let showed_placeholder = !cfg.pending_text.is_empty();
            {
                let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
                st.next_gen = st.next_gen.wrapping_add(1).max(1);
                let gen_id = st.next_gen;
                st.comp_gen.insert(component, gen_id);
                st.in_flight.insert(
                    req_id,
                    AsyncTarget {
                        req_id,
                        hook_id,
                        component,
                        gc_handle,
                        generation: gen_id,
                        orig_text: cleaned.to_owned(),
                        raw_orig_text: raw.to_owned(),
                        pending_text: cfg.pending_text.clone(),
                        display_mode: cfg.display_mode,
                        showed_placeholder,
                        created_at: Instant::now(),
                    },
                );
                sync_async_flags(&st);
            }
            if !ipc::send(&Msg::EmbedRequest {
                req_id,
                hook_id,
                ctx,
                ctx2,
                text: cleaned.to_owned(),
            }) {
                let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
                if let Some(removed) = st.in_flight.remove(&req_id) {
                    if removed.gc_handle != 0 {
                        st.handles_to_free.push(removed.gc_handle);
                    }
                    sync_async_flags(&st);
                }
                return ProcessOutcome::KeepOriginal;
            }
            return if showed_placeholder {
                ProcessOutcome::Replace {
                    text: cfg.pending_text.clone(),
                    apply_font: !cfg.pending_text.is_ascii(),
                }
            } else {
                ProcessOutcome::KeepOriginal
            };
        }
    }

    let wait = Duration::from_millis(cfg.wait_ms.max(1) as u64);
    let req_id = NEXT_REQ.fetch_add(1, Ordering::Relaxed);
    PENDING.lock().unwrap_or_else(|p| p.into_inner()).insert(req_id, None);
    if !ipc::send(&Msg::EmbedRequest { req_id, hook_id, ctx, ctx2, text: cleaned.to_owned() }) {
        PENDING.lock().unwrap_or_else(|p| p.into_inner()).remove(&req_id);
        return ProcessOutcome::KeepOriginal;
    }

    let wait_start = Instant::now();
    let deadline = wait_start + wait;
    let mut timed_out = false;
    let mut pending = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    let reply = loop {
        if let Some(Some(_)) = pending.get(&req_id) {
            break pending.remove(&req_id).flatten().flatten();
        }
        let now = Instant::now();
        if now >= deadline || SHUTDOWN.load(Ordering::Relaxed) {
            timed_out = now >= deadline && !SHUTDOWN.load(Ordering::Relaxed);
            pending.remove(&req_id);
            break None;
        }
        pending = PENDING_CV.wait_timeout(pending, deadline - now).unwrap_or_else(|p| p.into_inner()).0;
    };
    drop(pending);
    let wait_elapsed = wait_start.elapsed();
    let wait_us = wait_elapsed.as_micros() as u64;

    if timed_out {
        push_late_pending(req_id, cleaned.to_owned());
    }

    let slow_warn = !timed_out && wait_elapsed.as_millis() * 100 >= wait.as_millis() * 80;
    {
        let mut cb = CIRCUIT.lock().unwrap_or_else(|p| p.into_inner());
        if timed_out {
            cb.consecutive_timeouts = cb.consecutive_timeouts.saturating_add(1);
            if cb.consecutive_timeouts >= CB_THRESHOLD {
                cb.open_until = Some(Instant::now() + CB_OPEN_DURATION);
            }
        } else {
            cb.consecutive_timeouts = 0;
            cb.open_until = None;
        }

        if timed_out || slow_warn {
            let now = Instant::now();
            let allow = cb
                .last_telem_at
                .is_none_or(|prev| now.duration_since(prev) >= TELEMETRY_RATE_LIMIT);
            if allow {
                let sup = cb.suppressed_telem;
                cb.suppressed_telem = 0;
                cb.last_telem_at = Some(now);
                drop(cb);
                let (lvl, prefix) = if timed_out {
                    ("error", "embed wait timeout")
                } else {
                    ("warn", "embed wait > 80% of wait_ms")
                };
                crate::telemetry(
                    "IPC_PIPE",
                    "embed::process",
                    wait_us,
                    lvl,
                    &format!("{prefix} | hook_id={hook_id} wait_ms={} suppressed={sup}", wait.as_millis()),
                );
            } else {
                cb.suppressed_telem = cb.suppressed_telem.saturating_add(1);
            }
        }
    }

    match reply {
        Some(t) if !t.is_empty() && t != cleaned => {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .insert(cleaned.to_owned(), Some(t.clone()), None);
            let Some(out) = format_display(cfg.display_mode, cleaned, &t) else {
                return ProcessOutcome::KeepOriginal;
            };
            remember_output(&out);
            ProcessOutcome::Replace { text: out, apply_font: true }
        }
        Some(_) => {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .insert(cleaned.to_owned(), None, None);
            ProcessOutcome::KeepOriginal
        }
        None => {
            if !SHUTDOWN.load(Ordering::Relaxed) {
                let cooldown = if timed_out {
                    TIMEOUT_CACHE_COOLDOWN
                } else {
                    ERROR_CACHE_COOLDOWN
                };
                RECENT_TRANSLATIONS
                    .lock()
                    .unwrap_or_else(|p| p.into_inner())
                    .insert(cleaned.to_owned(), None, Some(Instant::now() + cooldown));
            }
            ProcessOutcome::KeepOriginal
        }
    }
}

/// Blocking convenience wrapper used in unit tests.
#[cfg(test)]
pub fn process(
    hook_id: u64,
    ctx_fn: impl FnOnce() -> u64,
    ctx2: u64,
    component: usize,
    text: &str,
) -> Option<String> {
    match process_unity(hook_id, ctx_fn, ctx2, component, text, text, || 0) {
        ProcessOutcome::Replace { text, .. } => Some(text),
        ProcessOutcome::KeepOriginal => None,
    }
}

pub fn complete(req_id: u64, text: Option<String>) {
    {
        let mut pending = PENDING.lock().unwrap_or_else(|p| p.into_inner());
        if let Some(slot) = pending.get_mut(&req_id) {
            *slot = Some(text);
            PENDING_CV.notify_all();
            return;
        }
    }

    let async_target = {
        let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
        let removed = st.in_flight.remove(&req_id);
        if removed.is_some() {
            sync_async_flags(&st);
        }
        removed
    };
    if let Some(target) = async_target {
        match text {
            Some(t) if !t.is_empty() && t != target.orig_text => {
                RECENT_TRANSLATIONS
                    .lock()
                    .unwrap_or_else(|p| p.into_inner())
                    .insert(target.orig_text.clone(), Some(t.clone()), None);
                let out = format_display(target.display_mode, &target.orig_text, &t);
                let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
                let still_current = st.comp_gen.get(&target.component).copied() == Some(target.generation);
                if still_current {
                    if let Some(out) = out {
                        st.ready.push_back(ReadyUpdate {
                            hook_id: target.hook_id,
                            component: target.component,
                            gc_handle: target.gc_handle,
                            text: out,
                            apply_font: true,
                        });
                    } else if target.gc_handle != 0 {
                        st.handles_to_free.push(target.gc_handle);
                    }
                } else if target.gc_handle != 0 {
                    st.handles_to_free.push(target.gc_handle);
                }
                sync_async_flags(&st);
            }
            _ => {
                if !SHUTDOWN.load(Ordering::Relaxed) {
                    RECENT_TRANSLATIONS
                        .lock()
                        .unwrap_or_else(|p| p.into_inner())
                        .insert(target.orig_text.clone(), None, Some(Instant::now() + ERROR_CACHE_COOLDOWN));
                }
                let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
                let still_current = st.comp_gen.get(&target.component).copied() == Some(target.generation);
                if target.showed_placeholder && still_current {
                    st.ready.push_back(ReadyUpdate {
                        hook_id: target.hook_id,
                        component: target.component,
                        gc_handle: target.gc_handle,
                        text: target.raw_orig_text,
                        apply_font: false,
                    });
                } else if target.gc_handle != 0 {
                    st.handles_to_free.push(target.gc_handle);
                }
                sync_async_flags(&st);
            }
        }
        return;
    }

    let orig = {
        let mut late = LATE_PENDING.lock().unwrap_or_else(|p| p.into_inner());
        late.iter()
            .position(|(id, _)| *id == req_id)
            .and_then(|idx| late.remove(idx))
            .map(|(_, s)| s)
    };
    if let (Some(orig), Some(t)) = (orig, text) {
        if !t.is_empty() && t != orig {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .insert(orig, Some(t), None);
        }
    }
}

pub(crate) fn take_async_work() -> (Vec<ReadyUpdate>, Vec<u32>) {
    let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
    let updates: Vec<ReadyUpdate> = st.ready.drain(..).collect();
    let frees = std::mem::take(&mut st.handles_to_free);
    sync_async_flags(&st);
    (updates, frees)
}

pub(crate) fn reap_expired_async() {
    let now = Instant::now();
    let mut st = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
    let expired_ids: Vec<u64> = st
        .in_flight
        .iter()
        .filter_map(|(&rid, t)| (now.duration_since(t.created_at) >= ASYNC_TTL).then_some(rid))
        .collect();
    if expired_ids.is_empty() {
        return;
    }
    for rid in expired_ids {
        if let Some(t) = st.in_flight.remove(&rid) {
            let still_current = st.comp_gen.get(&t.component).copied() == Some(t.generation);
            if t.showed_placeholder && still_current {
                st.ready.push_back(ReadyUpdate {
                    hook_id: t.hook_id,
                    component: t.component,
                    gc_handle: t.gc_handle,
                    text: t.raw_orig_text,
                    apply_font: false,
                });
            } else if t.gc_handle != 0 {
                st.handles_to_free.push(t.gc_handle);
            }
        }
    }
    sync_async_flags(&st);
}

fn clear_async_state() {
    let mut guard = ASYNC.lock().unwrap_or_else(|p| p.into_inner());
    let st = &mut *guard;
    for (_, t) in st.in_flight.drain() {
        if t.gc_handle != 0 {
            st.handles_to_free.push(t.gc_handle);
        }
    }
    for u in st.ready.drain(..) {
        if u.gc_handle != 0 {
            st.handles_to_free.push(u.gc_handle);
        }
    }
    st.comp_gen.clear();
    sync_async_flags(st);
}

/// Wakes every waiting game thread so unload never blocks the game.
pub fn release_all() {
    SHUTDOWN.store(true, Ordering::SeqCst);
    clear_async_state();
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
    LAST_SENT_TEXT.lock().unwrap_or_else(|p| p.into_inner()).clear();
    let _guard = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    PENDING_CV.notify_all();
}

/// Resets embed state for a new pipe session after re-attach.
pub fn reset_for_session() {
    SHUTDOWN.store(false, Ordering::SeqCst);
    clear_async_state();
    ENABLED.lock().unwrap_or_else(|p| p.into_inner()).clear();
    ENABLED_HOOK_MASK.store(0, Ordering::Release);
    ENABLED_HIGH_HOOKS.store(false, Ordering::Release);
    ENABLED_GEN.fetch_add(1, Ordering::AcqRel);
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    LAST_SENT_TEXT.lock().unwrap_or_else(|p| p.into_inner()).clear();
    {
        let mut own = OWN_OUTPUTS.lock().unwrap_or_else(|p| p.into_inner());
        own.queue.clear();
        own.set.clear();
        HAS_OWN_OUTPUTS.store(false, Ordering::Release);
    }
    PENDING.lock().unwrap_or_else(|p| p.into_inner()).clear();
    LATE_PENDING.lock().unwrap_or_else(|p| p.into_inner()).clear();
    *CIRCUIT.lock().unwrap_or_else(|p| p.into_inner()) = CircuitState::default();
}

pub fn is_shutting_down() -> bool {
    SHUTDOWN.load(Ordering::Relaxed)
}

#[cfg(test)]
pub(crate) fn test_guard() -> std::sync::MutexGuard<'static, ()> {
    static TEST_LOCK: LazyLock<Mutex<()>> = LazyLock::new(Default::default);
    TEST_LOCK.lock().unwrap_or_else(|p| p.into_inner())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn has_translatable_letters_filters_non_translatable() {
        assert!(!has_translatable_letters(""));
        assert!(!has_translatable_letters("12345 / 60"));
        assert!(!has_translatable_letters("... --- !!!"));
        assert!(!has_translatable_letters("<color=red>100%</color>"));
        assert!(!has_translatable_letters("مرحبا بالعالم"));
        assert!(has_translatable_letters("<color=red>HP:</color> 50"));
        assert!(has_translatable_letters("Start Game"));
        assert!(has_translatable_letters("日本語テスト"));
    }

    #[test]
    fn disabled_hook_returns_none_even_if_cached() {
        let _guard = test_guard();
        reset_for_session();
        RECENT_TRANSLATIONS
            .lock()
            .unwrap()
            .insert("Hello".to_owned(), Some("مرحبا".to_owned()), None);
        assert!(process(999, || 123, 0, 0, "Hello").is_none());
        reset_for_session();
    }

    #[test]
    fn circuit_breaker_skips_when_open_and_resets() {
        let _guard = test_guard();
        reset_for_session();
        set_enabled(1, 10, 0, true);
        {
            let mut cb = CIRCUIT.lock().unwrap();
            cb.consecutive_timeouts = CB_THRESHOLD;
            cb.open_until = Some(Instant::now() + Duration::from_secs(30));
        }
        // Should return None immediately without calling IPC or waiting.
        assert!(process(1, || 10, 0, 0, "Hello World").is_none());
        reset_for_session();
        let cb = CIRCUIT.lock().unwrap();
        assert_eq!(cb.consecutive_timeouts, 0);
        assert!(cb.open_until.is_none());
    }

    #[test]
    fn late_reply_warms_recent_cache() {
        let _guard = test_guard();
        reset_for_session();
        LATE_PENDING.lock().unwrap().push_back((777, "Late line".to_owned()));
        complete(777, Some("سطر متأخر".to_owned()));
        let cached = RECENT_TRANSLATIONS.lock().unwrap().get("Late line");
        assert_eq!(cached, Some(Some("سطر متأخر".to_owned())));
        reset_for_session();
    }

    #[test]
    fn disabled_component_bounds_ctx_reprobes_when_other_ctx_enabled() {
        let _guard = test_guard();
        reset_for_session();
        set_enabled(1, 999, 0, true);
        let mut calls = 0usize;
        for i in 0..50 {
            let line = format!("HP {i}");
            assert!(process(1, || {
                calls += 1;
                111
            }, 0, 0xABCD, &line).is_none());
        }
        assert!(calls <= (MAX_DISABLED_REPROBES as usize) + 1, "expected <= 3 calls, got {calls}");
        reset_for_session();
    }

    #[test]
    fn disabled_component_skips_reprobe_when_text_unchanged_and_picks_up_new_enable() {
        let _guard = test_guard();
        reset_for_session();
        {
            let mut cb = CIRCUIT.lock().unwrap();
            cb.consecutive_timeouts = CB_THRESHOLD;
            cb.open_until = Some(Instant::now() + Duration::from_secs(30));
        }
        let mut calls = 0usize;
        for _ in 0..20 {
            assert!(process(1, || {
                calls += 1;
                111
            }, 0, 0xBEEF, "Press E").is_none());
        }
        assert_eq!(calls, 1);

        set_enabled(1, 222, 0, true);
        assert!(process(1, || {
            calls += 1;
            222
        }, 0, 0xBEEF, "Press E").is_none());
        assert_eq!(calls, 2);

        // Subsequent calls hit cached enabled component entry directly.
        assert!(process(1, || {
            calls += 1;
            222
        }, 0, 0xBEEF, "Press F").is_none());
        assert_eq!(calls, 2);
        reset_for_session();
    }

    #[test]
    fn no_enabled_hooks_never_reprobes_cached_component() {
        let _guard = test_guard();
        reset_for_session();
        let mut calls = 0usize;
        for i in 0..20 {
            let line = format!("Quest step {i}");
            assert!(process(1, || {
                calls += 1;
                111
            }, 0, 0xCAFE, &line).is_none());
        }
        assert_eq!(calls, 1);
        reset_for_session();
    }

    fn decode_sent_req_id(rx: &std::sync::mpsc::Receiver<Vec<u8>>) -> u64 {
        let frame = rx.try_recv().expect("expected EmbedRequest frame");
        let msg = khutaf_proto::Msg::decode(&frame[4..]).expect("decode frame");
        match msg {
            khutaf_proto::Msg::EmbedRequest { req_id, .. } => req_id,
            other => panic!("unexpected msg: {other:?}"),
        }
    }

    #[test]
    fn async_embed_returns_placeholder_then_queues_ready_update() {
        let _guard = test_guard();
        reset_for_session();
        let rx = ipc::install_test_sink();
        set_frame_tick_active(true);
        set_settings(Settings {
            wait_ms: 1000,
            non_blocking: true,
            pending_text: "...".into(),
            ..Default::default()
        });
        set_enabled(1, 55, 0, true);

        let mut pin_calls = 0usize;
        let out = process_unity(1, || 55, 0, 0x1000, "Hello Rohen", "Hello Rohen", || {
            pin_calls += 1;
            701
        });
        assert_eq!(out, ProcessOutcome::Replace { text: "...".into(), apply_font: false });
        assert_eq!(pin_calls, 1);
        let req_id = decode_sent_req_id(&rx);

        // Repeat typewriter update with same text reuses in-flight request without re-pinning.
        let dup = process_unity(1, || 55, 0, 0x1000, "Hello Rohen", "Hello Rohen", || {
            pin_calls += 1;
            702
        });
        assert_eq!(dup, ProcessOutcome::Replace { text: "...".into(), apply_font: false });
        assert_eq!(pin_calls, 1);
        assert!(rx.try_recv().is_err());

        complete(req_id, Some("مرحبا روهين".into()));
        let (updates, frees) = take_async_work();
        assert!(frees.is_empty());
        assert_eq!(updates.len(), 1);
        assert_eq!(updates[0].gc_handle, 701);
        assert_eq!(updates[0].text, "مرحبا روهين");
        assert!(updates[0].apply_font);

        ipc::clear_test_sink();
        set_frame_tick_active(false);
        set_settings(Settings::default());
        reset_for_session();
    }

    #[test]
    fn async_embed_restores_original_on_error_when_placeholder_shown() {
        let _guard = test_guard();
        reset_for_session();
        let rx = ipc::install_test_sink();
        set_frame_tick_active(true);
        set_settings(Settings {
            wait_ms: 1000,
            non_blocking: true,
            pending_text: "...".into(),
            ..Default::default()
        });
        set_enabled(1, 55, 0, true);

        let out = process_unity(1, || 55, 0, 0x2000, "Line", "<color=red>Line</color>", || 801);
        assert_eq!(out, ProcessOutcome::Replace { text: "...".into(), apply_font: false });
        let req_id = decode_sent_req_id(&rx);

        complete(req_id, None);
        let (updates, _) = take_async_work();
        assert_eq!(updates.len(), 1);
        assert_eq!(updates[0].gc_handle, 801);
        assert_eq!(updates[0].text, "<color=red>Line</color>");
        assert!(!updates[0].apply_font);

        ipc::clear_test_sink();
        set_frame_tick_active(false);
        set_settings(Settings::default());
        reset_for_session();
    }

    #[test]
    fn async_embed_supersedes_stale_line_on_same_component() {
        let _guard = test_guard();
        reset_for_session();
        let rx = ipc::install_test_sink();
        set_frame_tick_active(true);
        set_settings(Settings {
            wait_ms: 1000,
            non_blocking: true,
            pending_text: "...".into(),
            ..Default::default()
        });
        set_enabled(1, 55, 0, true);

        let _ = process_unity(1, || 55, 0, 0x3000, "Line 1", "Line 1", || 901);
        let req1 = decode_sent_req_id(&rx);
        let _ = process_unity(1, || 55, 0, 0x3000, "Line 2", "Line 2", || 902);
        let req2 = decode_sent_req_id(&rx);

        // Completing stale req1 frees handle 901 and warms cache, without overwriting Line 2.
        complete(req1, Some("السطر الأول".into()));
        let (updates, frees) = take_async_work();
        assert!(updates.is_empty());
        assert_eq!(frees, vec![901]);
        assert_eq!(
            RECENT_TRANSLATIONS.lock().unwrap().get("Line 1"),
            Some(Some("السطر الأول".to_owned()))
        );

        complete(req2, Some("السطر الثاني".into()));
        let (updates2, _) = take_async_work();
        assert_eq!(updates2.len(), 1);
        assert_eq!(updates2[0].gc_handle, 902);
        assert_eq!(updates2[0].text, "السطر الثاني");

        ipc::clear_test_sink();
        set_frame_tick_active(false);
        set_settings(Settings::default());
        reset_for_session();
    }

    #[test]
    fn async_embed_empty_pending_keeps_original_and_skips_restore_on_error() {
        let _guard = test_guard();
        reset_for_session();
        let rx = ipc::install_test_sink();
        set_frame_tick_active(true);
        set_settings(Settings {
            wait_ms: 1000,
            non_blocking: true,
            pending_text: "".into(),
            ..Default::default()
        });
        set_enabled(1, 55, 0, true);

        let out = process_unity(1, || 55, 0, 0x4000, "Item Name", "Item Name", || 1001);
        assert_eq!(out, ProcessOutcome::KeepOriginal);
        let req_id = decode_sent_req_id(&rx);

        complete(req_id, None);
        let (updates, frees) = take_async_work();
        assert!(updates.is_empty());
        assert_eq!(frees, vec![1001]);

        ipc::clear_test_sink();
        set_frame_tick_active(false);
        set_settings(Settings::default());
        reset_for_session();
    }

    #[test]
    fn all_display_modes_format_and_bypass_correctly() {
        let _guard = test_guard();
        reset_for_session();
        let _rx = ipc::install_test_sink();
        set_enabled(1, 55, 0, true);
        let seed_cache = || {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap()
                .insert("Hello World".to_owned(), Some("مرحبا".to_owned()), None);
        };

        // Mode 0: Translation Only
        set_settings(Settings { display_mode: 0, ..Default::default() });
        seed_cache();
        assert_eq!(
            process_unity(1, || 55, 0, 0x5000, "Hello World", "Hello World", || 0),
            ProcessOutcome::Replace { text: "مرحبا".to_owned(), apply_font: true }
        );

        // Mode 1: Original + Translation
        set_settings(Settings { display_mode: 1, ..Default::default() });
        seed_cache();
        assert_eq!(
            process_unity(1, || 55, 0, 0x5001, "Hello World", "Hello World", || 0),
            ProcessOutcome::Replace { text: "Hello World\nمرحبا".to_owned(), apply_font: true }
        );

        // Mode 2: Translation + Original
        set_settings(Settings { display_mode: 2, ..Default::default() });
        seed_cache();
        assert_eq!(
            process_unity(1, || 55, 0, 0x5002, "Hello World", "Hello World", || 0),
            ProcessOutcome::Replace { text: "مرحبا\nHello World".to_owned(), apply_font: true }
        );

        // Mode 3: Original Only (Bypass)
        set_settings(Settings { display_mode: 3, ..Default::default() });
        seed_cache();
        assert_eq!(
            process_unity(1, || 55, 0, 0x5003, "Hello World", "Hello World", || 0),
            ProcessOutcome::KeepOriginal
        );

        assert_eq!(format_display(0, "Orig", "Trans"), Some("Trans".to_owned()));
        assert_eq!(format_display(1, "Orig", "Trans"), Some("Orig\nTrans".to_owned()));
        assert_eq!(format_display(2, "Orig", "Trans"), Some("Trans\nOrig".to_owned()));
        assert_eq!(format_display(3, "Orig", "Trans"), None);

        ipc::clear_test_sink();
        set_settings(Settings::default());
        reset_for_session();
    }
}
