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

fn remember_output(text: &str) {
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

/// Reports hooked text to the host and, when embedding is enabled for the
/// thread, blocks up to `wait_ms` for a replacement. `None` = keep original.
pub fn process(
    hook_id: u64,
    ctx_fn: impl FnOnce() -> u64,
    ctx2: u64,
    component: usize,
    text: &str,
) -> Option<String> {
    if text.trim().is_empty() || is_own_output(text) {
        return None;
    }

    let display_mode = DISPLAY_MODE.load(Ordering::Relaxed);
    let clear_text = CLEAR_TEXT.load(Ordering::Relaxed);
    let (ctx, enabled, unchanged_disabled) =
        resolve_component(hook_id, ctx_fn, ctx2, component, fnv1a_64(text.as_bytes()));
    if unchanged_disabled {
        return None;
    }
    if !enabled || display_mode == 3 || text.chars().count() <= 1 {
        if has_translatable_letters(text) {
            send_text_dedup(hook_id, ctx, ctx2, text);
        }
        return None;
    }
    if clear_text {
        send_text_dedup(hook_id, ctx, ctx2, text);
        return Some(" ".to_owned());
    }
    if !has_translatable_letters(text) {
        return None;
    }
    let cfg = settings();
    if let Some(cached) = RECENT_TRANSLATIONS
        .lock()
        .unwrap_or_else(|p| p.into_inner())
        .get(text)
    {
        let cached = cached?;
        let out = format_display(cfg.display_mode, text, &cached)?;
        remember_output(&out);
        return Some(out);
    }

    {
        let cb = CIRCUIT.lock().unwrap_or_else(|p| p.into_inner());
        if let Some(until) = cb.open_until {
            if Instant::now() < until {
                return None;
            }
        }
    }

    let wait = Duration::from_millis(cfg.wait_ms.max(1) as u64);
    let req_id = NEXT_REQ.fetch_add(1, Ordering::Relaxed);
    PENDING.lock().unwrap_or_else(|p| p.into_inner()).insert(req_id, None);
    if !ipc::send(&Msg::EmbedRequest { req_id, hook_id, ctx, ctx2, text: text.to_owned() }) {
        PENDING.lock().unwrap_or_else(|p| p.into_inner()).remove(&req_id);
        return None;
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
        let mut late = LATE_PENDING.lock().unwrap_or_else(|p| p.into_inner());
        if late.len() >= MAX_LATE_PENDING {
            late.pop_front();
        }
        late.push_back((req_id, text.to_owned()));
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
        Some(t) if !t.is_empty() && t != text => {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .insert(text.to_owned(), Some(t.clone()), None);
            let out = format_display(cfg.display_mode, text, &t)?;
            remember_output(&out);
            Some(out)
        }
        Some(_) => {
            RECENT_TRANSLATIONS
                .lock()
                .unwrap_or_else(|p| p.into_inner())
                .insert(text.to_owned(), None, None);
            None
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
                    .insert(text.to_owned(), None, Some(Instant::now() + cooldown));
            }
            None
        }
    }
}

pub fn complete(req_id: u64, text: Option<String>) {
    let mut pending = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    if let Some(slot) = pending.get_mut(&req_id) {
        *slot = Some(text);
        PENDING_CV.notify_all();
        return;
    }
    drop(pending);

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

/// Wakes every waiting game thread so unload never blocks the game.
pub fn release_all() {
    SHUTDOWN.store(true, Ordering::SeqCst);
    RECENT_TRANSLATIONS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    COMPONENT_CTX.lock().unwrap_or_else(|p| p.into_inner()).clear();
    LAST_SENT_TEXT.lock().unwrap_or_else(|p| p.into_inner()).clear();
    let _guard = PENDING.lock().unwrap_or_else(|p| p.into_inner());
    PENDING_CV.notify_all();
}

/// Resets embed state for a new pipe session after re-attach.
pub fn reset_for_session() {
    SHUTDOWN.store(false, Ordering::SeqCst);
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
}
