//! Unity runtime detection, text hooking (Mono & IL2CPP), and font injection.

pub mod font;
pub mod il2cpp;
pub mod mono;

use std::borrow::Cow;
use std::cell::RefCell;
use std::collections::{HashMap, HashSet};
use std::ffi::{c_char, c_void, CString};
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::{LazyLock, Mutex, OnceLock};
use std::time::{Duration, Instant};

use khutaf_proto::{level, Msg};
use windows_sys::Win32::Foundation::HMODULE;
use windows_sys::Win32::System::LibraryLoader::{GetModuleHandleW, GetProcAddress};
use windows_sys::Win32::System::Memory::{VirtualQuery, MEMORY_BASIC_INFORMATION};

use crate::{embed, hooks, ipc};

pub const HOOK_TMP_SET_TEXT: u64 = 1;
pub const HOOK_TMP_SETTEXT: u64 = 2;
pub const HOOK_UI_TEXT_SET_TEXT: u64 = 3;
pub const HOOK_TEXTMESH_SET_TEXT: u64 = 4;
pub const HOOK_TEXTMESHPRO_SET_TEXT: u64 = 5;
pub const HOOK_TEXTMESHPRO_UGUI_SET_TEXT: u64 = 6;
pub const HOOK_TEXTMESHPRO_UGUI_SETTEXT: u64 = 7;
pub const HOOK_UIE_TEXTELEMENT_SET_TEXT: u64 = 8;
pub const HOOK_UIE_TEXTFIELD_SET_VALUE: u64 = 9;
pub const HOOK_GUITEXT_SET_TEXT: u64 = 10;
pub const HOOK_NGUILABEL_SET_TEXT: u64 = 11;
pub const HOOK_TMP_STATIC_UI: u64 = 12;
pub const HOOK_UI_TEXT_STATIC_UI: u64 = 13;
pub const HOOK_FEBUCCI_TEXTANIMATOR_SET_TEXT: u64 = 14;

pub(crate) fn is_tmp_hook(hook_id: u64) -> bool {
    matches!(
        hook_id,
        HOOK_TMP_SET_TEXT
            | HOOK_TMP_SETTEXT
            | HOOK_TEXTMESHPRO_SET_TEXT
            | HOOK_TEXTMESHPRO_UGUI_SET_TEXT
            | HOOK_TEXTMESHPRO_UGUI_SETTEXT
            | HOOK_TMP_STATIC_UI
    )
}

pub(crate) fn is_tmp_like_tags(hook_id: u64) -> bool {
    is_tmp_hook(hook_id) || hook_id == HOOK_FEBUCCI_TEXTANIMATOR_SET_TEXT
}

pub(crate) fn is_ui_text_hook(hook_id: u64) -> bool {
    matches!(hook_id, HOOK_UI_TEXT_SET_TEXT | HOOK_UI_TEXT_STATIC_UI)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Engine {
    UnityMono,
    UnityIl2cpp,
    Unity,
    Unknown,
}

impl Engine {
    pub fn name(self) -> &'static str {
        match self {
            Engine::UnityMono => "unity-mono",
            Engine::UnityIl2cpp => "unity-il2cpp",
            Engine::Unity => "unity",
            Engine::Unknown => "unknown",
        }
    }
}

pub(crate) fn module_handle(name: &str) -> HMODULE {
    let wide: Vec<u16> = name.encode_utf16().chain(Some(0)).collect();
    unsafe { GetModuleHandleW(wide.as_ptr()) }
}

pub(crate) fn proc_addr<T: Copy>(module: HMODULE, sym: &str) -> Option<T> {
    let c = CString::new(sym).ok()?;
    let ptr = unsafe { GetProcAddress(module, c.as_ptr() as *const u8) }?;
    if std::mem::size_of::<T>() != std::mem::size_of::<usize>() {
        return None;
    }
    let raw = ptr as usize;
    Some(unsafe { std::mem::transmute_copy::<usize, T>(&raw) })
}

pub(crate) fn cstr_to_string(p: *const c_char) -> String {
    if p.is_null() {
        return String::new();
    }
    unsafe { std::ffi::CStr::from_ptr(p).to_string_lossy().into_owned() }
}

#[repr(C)]
struct RuntimeFunction {
    begin_addr: u32,
    end_addr: u32,
    unwind_info: u32,
}

type CaptureBackTraceFn = unsafe extern "system" fn(u32, u32, *mut *mut c_void, *mut u32) -> u16;
type LookupFnEntryFn = unsafe extern "system" fn(u64, *mut u64, *mut c_void) -> *const RuntimeFunction;
static CAPTURE_BT: OnceLock<Option<CaptureBackTraceFn>> = OnceLock::new();
static LOOKUP_FN_ENTRY: OnceLock<Option<LookupFnEntryFn>> = OnceLock::new();

const MAX_FRAME_CTX_CACHE: usize = 2048;

thread_local! {
    static FRAME_CTX_CACHE: RefCell<HashMap<usize, u64>> = RefCell::new(HashMap::new());
}

/// Returns the caller's function entry RVA (via `RtlLookupFunctionEntry`, falling
/// back to raw return offset) skipping stack frames inside `khutaf-hook64.dll`.
pub(crate) fn caller_rel_ctx() -> u64 {
    let Some(capture) = *CAPTURE_BT.get_or_init(|| {
        let k32 = module_handle("kernel32.dll");
        if k32.is_null() {
            return None;
        }
        proc_addr::<CaptureBackTraceFn>(k32, "RtlCaptureStackBackTrace")
    }) else {
        return 0;
    };
    let lookup = *LOOKUP_FN_ENTRY.get_or_init(|| {
        let k32 = module_handle("kernel32.dll");
        if k32.is_null() {
            return None;
        }
        proc_addr::<LookupFnEntryFn>(k32, "RtlLookupFunctionEntry")
    });

    let mut frames = [std::ptr::null_mut(); 8];
    let count = unsafe { capture(1, frames.len() as u32, frames.as_mut_ptr(), std::ptr::null_mut()) } as usize;
    let self_base = crate::module_base();
    for &frame in &frames[..count.min(frames.len())] {
        if frame.is_null() {
            continue;
        }
        let addr = frame as usize;
        let cached = FRAME_CTX_CACHE.with(|c| c.borrow().get(&addr).copied());
        if let Some(rva) = cached {
            if rva == 0 {
                continue;
            }
            return rva;
        }

        let mut mbi: MEMORY_BASIC_INFORMATION = unsafe { std::mem::zeroed() };
        let res = unsafe {
            VirtualQuery(
                frame as *const c_void,
                &mut mbi,
                std::mem::size_of::<MEMORY_BASIC_INFORMATION>(),
            )
        };
        if res == 0 || mbi.AllocationBase.is_null() {
            continue;
        }
        let alloc_base = mbi.AllocationBase as usize;
        if alloc_base == self_base {
            FRAME_CTX_CACHE.with(|c| {
                let mut m = c.borrow_mut();
                if m.len() >= MAX_FRAME_CTX_CACHE {
                    m.clear();
                }
                m.insert(addr, 0);
            });
            continue;
        }
        let mut rva = (addr.wrapping_sub(alloc_base)) as u64;
        if let Some(lookup_fn) = lookup {
            let mut image_base: u64 = 0;
            let rf = unsafe { lookup_fn(frame as u64, &mut image_base, std::ptr::null_mut()) };
            if !rf.is_null() && image_base != 0 && image_base != self_base as u64 {
                rva = unsafe { (*rf).begin_addr as u64 };
            }
        }
        FRAME_CTX_CACHE.with(|c| {
            let mut m = c.borrow_mut();
            if m.len() >= MAX_FRAME_CTX_CACHE {
                m.clear();
            }
            m.insert(addr, rva);
        });
        return rva;
    }
    0
}

fn find_ascii_ci(haystack: &str, needle: &str) -> Option<usize> {
    let hb = haystack.as_bytes();
    let nb = needle.as_bytes();
    if nb.is_empty() || nb.len() > hb.len() {
        return None;
    }
    let first_lo = nb[0].to_ascii_lowercase();
    let first_hi = nb[0].to_ascii_uppercase();
    let mut i = 0;
    let end = hb.len() - nb.len();
    while i <= end {
        let b = hb[i];
        if (b == first_lo || b == first_hi) && hb[i..i + nb.len()].eq_ignore_ascii_case(nb) {
            return Some(i);
        }
        i += 1;
    }
    None
}

/// Zero-allocation UTF-16 pre-filter matching `embed::has_translatable_letters` for hot detours.
pub(crate) fn utf16_has_translatable_letters(units: &[u16]) -> bool {
    if units.len() <= 1 {
        return false;
    }
    let mut in_tag = false;
    let mut has_letter = false;
    let mut i = 0;
    while i < units.len() {
        let u = units[i];
        if in_tag {
            if u == b'>' as u16 {
                in_tag = false;
            }
            i += 1;
            continue;
        }
        if u == b'<' as u16 && units[i + 1..].contains(&(b'>' as u16)) {
            in_tag = true;
            i += 1;
            continue;
        }
        if matches!(u, 0xFB50..=0xFDFF | 0xFE70..=0xFEFF) {
            return false;
        }
        if u < 0x80 {
            if (u as u8).is_ascii_alphabetic() {
                has_letter = true;
            }
        } else if !(0xD800..=0xDFFF).contains(&u) {
            if let Some(ch) = char::from_u32(u as u32) {
                let code = ch as u32;
                let is_arabic = matches!(
                    code,
                    0x0600..=0x06FF | 0x0750..=0x077F | 0x08A0..=0x08FF | 0xFB50..=0xFDFF | 0xFE70..=0xFEFF
                );
                if ch.is_alphabetic() && !is_arabic {
                    has_letter = true;
                }
            }
        }
        i += 1;
    }
    has_letter
}

fn strip_tag_prefix<'a>(s: &'a str, prefix: &str) -> Cow<'a, str> {
    let Some(first) = find_ascii_ci(s, prefix) else {
        return Cow::Borrowed(s);
    };
    let mut out = String::with_capacity(s.len());
    out.push_str(&s[..first]);
    let mut rest = &s[first + prefix.len()..];
    if let Some(end) = rest.find('>') {
        rest = &rest[end + 1..];
    } else {
        out.push_str(&s[first..]);
        return Cow::Owned(out);
    }
    while let Some(start) = find_ascii_ci(rest, prefix) {
        out.push_str(&rest[..start]);
        let after = &rest[start + prefix.len()..];
        if let Some(end) = after.find('>') {
            rest = &after[end + 1..];
        } else {
            out.push_str(&rest[start..]);
            rest = "";
            break;
        }
    }
    out.push_str(rest);
    Cow::Owned(out)
}

fn strip_exact_ci<'a>(s: &'a str, needle: &str) -> Cow<'a, str> {
    let Some(first) = find_ascii_ci(s, needle) else {
        return Cow::Borrowed(s);
    };
    let mut out = String::with_capacity(s.len());
    out.push_str(&s[..first]);
    let mut rest = &s[first + needle.len()..];
    while let Some(pos) = find_ascii_ci(rest, needle) {
        out.push_str(&rest[..pos]);
        rest = &rest[pos + needle.len()..];
    }
    out.push_str(rest);
    Cow::Owned(out)
}

/// Strips `<color=#00000000>` and its matching `</color>` used by typewriter effects
/// while leaving all visible `<color=...>` tags intact.
fn strip_typewriter_alpha_tags(s: &str) -> Cow<'_, str> {
    const TAG: &str = "<color=#00000000>";
    const CLOSE: &str = "</color>";
    if find_ascii_ci(s, TAG).is_none() {
        return Cow::Borrowed(s);
    }
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(pos) = find_ascii_ci(rest, TAG) {
        out.push_str(&rest[..pos]);
        rest = &rest[pos + TAG.len()..];

        let mut depth = 1usize;
        let mut cursor = 0usize;
        let mut matched_end = None;
        while cursor < rest.len() {
            let sub = &rest[cursor..];
            let next_open = find_ascii_ci(sub, "<color").filter(|&i| {
                let after = &sub[i + 6..];
                after.starts_with('=') || after.starts_with('>')
            });
            let next_close = find_ascii_ci(sub, CLOSE);
            match (next_open, next_close) {
                (Some(o), Some(c)) if o < c => {
                    depth += 1;
                    cursor += o + 6;
                }
                (_, Some(c)) => {
                    depth -= 1;
                    if depth == 0 {
                        matched_end = Some(cursor + c);
                        break;
                    }
                    cursor += c + CLOSE.len();
                }
                _ => break,
            }
        }
        if let Some(end) = matched_end {
            out.push_str(&rest[..end]);
            rest = &rest[end + CLOSE.len()..];
        }
    }
    out.push_str(rest);
    Cow::Owned(out)
}

/// Strips non-semantic TextMeshPro layout tags (`<noparse>`, `<line-height=...>`, `<sprite anim=...>`)
/// and optionally typewriter alpha tags (`<color=#00000000>...</color>`).
pub(crate) fn clean_tmp_tags(s: &str) -> Cow<'_, str> {
    if !s.contains('<') {
        return Cow::Borrowed(s);
    }
    let mut cur: Cow<'_, str> = Cow::Borrowed(s);
    if let Cow::Owned(next) = strip_exact_ci(&cur, "<noparse>") {
        cur = Cow::Owned(next);
    }
    if let Cow::Owned(next) = strip_exact_ci(&cur, "</noparse>") {
        cur = Cow::Owned(next);
    }
    if embed::strip_typewriter_tags() {
        if let Cow::Owned(next) = strip_typewriter_alpha_tags(&cur) {
            cur = Cow::Owned(next);
        }
    }
    if let Cow::Owned(next) = strip_tag_prefix(&cur, "<line-height=") {
        cur = Cow::Owned(next);
    }
    if let Cow::Owned(next) = strip_tag_prefix(&cur, "<sprite anim=") {
        cur = Cow::Owned(next);
    }
    cur
}

pub fn detect() -> Engine {
    if !module_handle("GameAssembly.dll").is_null() {
        Engine::UnityIl2cpp
    } else if ["mono-2.0-bdwgc.dll", "mono-2.0-sgen.dll", "mono.dll"]
        .iter()
        .any(|m| !module_handle(m).is_null())
    {
        Engine::UnityMono
    } else if !module_handle("UnityPlayer.dll").is_null() {
        Engine::Unity
    } else {
        Engine::Unknown
    }
}

static HOOKS_INSTALLED: AtomicBool = AtomicBool::new(false);
static INSTALLED_HOOKS: LazyLock<Mutex<Vec<(u64, String, String)>>> = LazyLock::new(Default::default);
static SEEN_META_KEYS: LazyLock<Mutex<HashSet<(u64, u64, u64)>>> = LazyLock::new(Default::default);
static SEEN_META_COMPS: LazyLock<Mutex<HashSet<(u64, usize)>>> = LazyLock::new(Default::default);

struct HierarchyMeta {
    resolved: bool,
    t_component: *mut c_void,
    m_comp_get_transform: *mut c_void,
    m_obj_get_name: *mut c_void,
    m_transform_get_parent: *mut c_void,
}

unsafe impl Send for HierarchyMeta {}

static HIERARCHY_META: LazyLock<Mutex<HierarchyMeta>> = LazyLock::new(|| {
    Mutex::new(HierarchyMeta {
        resolved: false,
        t_component: std::ptr::null_mut(),
        m_comp_get_transform: std::ptr::null_mut(),
        m_obj_get_name: std::ptr::null_mut(),
        m_transform_get_parent: std::ptr::null_mut(),
    })
});

pub fn reset_for_session() {
    SEEN_META_KEYS.lock().unwrap_or_else(|p| p.into_inner()).clear();
    SEEN_META_COMPS.lock().unwrap_or_else(|p| p.into_inner()).clear();
}

fn ensure_hierarchy_meta(rt: &dyn UnityRuntime, meta: &mut HierarchyMeta) {
    if meta.resolved {
        return;
    }
    meta.resolved = true;
    let t_obj = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Object");
    if !t_obj.is_null() {
        meta.m_obj_get_name = rt.find_method_in_hierarchy(t_obj, "get_name", 0);
    }
    let t_comp = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Component");
    meta.t_component = t_comp;
    if !t_comp.is_null() {
        meta.m_comp_get_transform = rt.find_method_in_hierarchy(t_comp, "get_transform", 0);
    }
    let t_tr = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Transform");
    if !t_tr.is_null() {
        meta.m_transform_get_parent = rt.find_method_in_hierarchy(t_tr, "get_parent", 0);
    }
}

fn sanitize_meta_segment(raw: &str) -> String {
    raw.chars()
        .map(|c| match c {
            '|' | '\r' | '\n' | '\t' => ' ',
            _ => c,
        })
        .collect::<String>()
        .trim()
        .to_owned()
}

fn resolve_component_hierarchy(rt: &dyn UnityRuntime, component: *mut c_void) -> Option<String> {
    if component.is_null() {
        return None;
    }
    let comp_cls = rt.object_get_class(component);
    if comp_cls.is_null() {
        return None;
    }
    let (t_comp, m_get_tr, m_get_name, m_get_parent) = {
        let mut meta = HIERARCHY_META.lock().unwrap_or_else(|p| p.into_inner());
        ensure_hierarchy_meta(rt, &mut meta);
        (
            meta.t_component,
            meta.m_comp_get_transform,
            meta.m_obj_get_name,
            meta.m_transform_get_parent,
        )
    };

    if !t_comp.is_null() && rt.class_is_assignable_from(t_comp, comp_cls) && !m_get_tr.is_null() && !m_get_name.is_null()
    {
        let mut tr = rt.invoke(m_get_tr, component, &mut []).unwrap_or(std::ptr::null_mut());
        let mut parts: Vec<String> = Vec::with_capacity(4);
        for _ in 0..4 {
            if tr.is_null() {
                break;
            }
            if let Some(name_ptr) = rt.invoke(m_get_name, tr, &mut []) {
                let seg = sanitize_meta_segment(&rt.string_to_rust(name_ptr));
                if !seg.is_empty() && !seg.eq_ignore_ascii_case("canvas (environment)") {
                    parts.push(seg);
                }
            }
            if m_get_parent.is_null() {
                break;
            }
            tr = rt.invoke(m_get_parent, tr, &mut []).unwrap_or(std::ptr::null_mut());
        }
        if !parts.is_empty() {
            parts.reverse();
            let mut path = parts.join("/");
            if path.chars().count() > 96 {
                path = path.chars().take(96).collect();
            }
            return Some(path);
        }
    }

    // Fallback for non-Component UI elements (e.g. UI Toolkit VisualElement.get_name).
    let m_name = rt.find_method_in_hierarchy(comp_cls, "get_name", 0);
    if !m_name.is_null() {
        if let Some(name_ptr) = rt.invoke(m_name, component, &mut []) {
            let seg = sanitize_meta_segment(&rt.string_to_rust(name_ptr));
            if !seg.is_empty() {
                return Some(seg.chars().take(96).collect());
            }
        }
    }
    None
}

/// Emits the Unity GameObject hierarchy path once per discovered `(hook_id, ctx, ctx2)`.
pub(crate) fn maybe_emit_component_meta(
    rt: &dyn UnityRuntime,
    component: *mut c_void,
    hook_id: u64,
    ctx: u64,
    ctx2: u64,
) {
    if component.is_null() || (ctx == 0 && ctx2 == 0) {
        return;
    }
    let key = (hook_id, ctx, ctx2);
    {
        let keys = SEEN_META_KEYS.lock().unwrap_or_else(|p| p.into_inner());
        if keys.contains(&key) {
            return;
        }
    }
    let comp_key = (hook_id, component as usize);
    {
        let mut comps = SEEN_META_COMPS.lock().unwrap_or_else(|p| p.into_inner());
        if !comps.insert(comp_key) {
            return;
        }
        if comps.len() >= 4096 {
            comps.clear();
        }
    }
    if let Some(path) = resolve_component_hierarchy(rt, component) {
        let should_send = {
            let mut keys = SEEN_META_KEYS.lock().unwrap_or_else(|p| p.into_inner());
            if keys.len() >= 2048 {
                keys.clear();
            }
            keys.insert(key)
        };
        if should_send {
            crate::hook_meta(hook_id, ctx, ctx2, &path);
        }
    }
}

pub(crate) fn record_and_emit_hook(hook_id: u64, code: &str, name: &str) {
    {
        let mut list = INSTALLED_HOOKS.lock().unwrap_or_else(|p| p.into_inner());
        if !list.iter().any(|(id, _, _)| *id == hook_id) {
            list.push((hook_id, code.to_owned(), name.to_owned()));
        }
    }
    ipc::send(&Msg::HookAdded {
        hook_id,
        code: code.into(),
        name: name.into(),
        embeddable: true,
    });
}

pub fn install(mut engine: Engine) {
    let start = Instant::now();
    if HOOKS_INSTALLED.load(Ordering::Acquire) {
        if let Err(e) = unsafe { hooks::enable_all() } {
            crate::telemetry(
                "HOOK_NATIVE",
                "unity::install",
                start.elapsed().as_micros() as u64,
                "error",
                &format!("re-enable hooks failed: {e}"),
            );
            return;
        }
        let hooks_list = INSTALLED_HOOKS.lock().unwrap_or_else(|p| p.into_inner()).clone();
        let count = hooks_list.len();
        for (hook_id, code, name) in hooks_list {
            ipc::send(&Msg::HookAdded { hook_id, code, name, embeddable: true });
        }
        crate::telemetry(
            "HOOK_NATIVE",
            "unity::install",
            start.elapsed().as_micros() as u64,
            "info",
            &format!("re-enabled {count} hook(s)"),
        );
        return;
    }

    if matches!(engine, Engine::Unknown) {
        crate::telemetry(
            "HOOK_NATIVE",
            "unity::install",
            start.elapsed().as_micros() as u64,
            "warn",
            "unknown engine; only Unity Mono/IL2CPP is supported",
        );
        return;
    }

    // Wait up to 10s for Unity assemblies to load (aborting immediately on detach).
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        if embed::is_shutting_down() {
            return;
        }
        if engine == Engine::Unity {
            engine = detect();
        }
        let ready = match engine {
            Engine::UnityMono => mono::is_ready(),
            Engine::UnityIl2cpp => il2cpp::is_ready(),
            _ => false,
        };
        if ready || Instant::now() >= deadline {
            break;
        }
        std::thread::sleep(Duration::from_millis(200));
    }

    if embed::is_shutting_down() {
        return;
    }

    let count = match engine {
        Engine::UnityMono => mono::install(),
        Engine::UnityIl2cpp => il2cpp::install(),
        _ => {
            crate::log(level::WARN, "Unity runtime (Mono/IL2CPP) did not initialize in time");
            0
        }
    };

    if count > 0 {
        HOOKS_INSTALLED.store(true, Ordering::Release);
        if let Err(e) = unsafe { hooks::enable_all() } {
            crate::telemetry(
                "HOOK_NATIVE",
                "unity::install",
                start.elapsed().as_micros() as u64,
                "error",
                &format!("enable hooks failed: {e}"),
            );
        } else {
            crate::telemetry(
                "HOOK_NATIVE",
                "unity::install",
                start.elapsed().as_micros() as u64,
                "info",
                &format!("installed {count} hook(s) | engine={}", engine.name()),
            );
        }
    } else {
        crate::telemetry(
            "HOOK_NATIVE",
            "unity::install",
            start.elapsed().as_micros() as u64,
            "warn",
            &format!("no text hooks found | engine={}", engine.name()),
        );
    }
}

/// Shared runtime abstraction used by `font.rs` so Mono and IL2CPP share font logic.
pub trait UnityRuntime {
    fn find_class(&self, preferred_asm: &str, ns: &str, name: &str) -> *mut c_void;
    fn find_method(&self, klass: *mut c_void, name: &str, param_count: i32) -> *mut c_void;
    fn find_method_param0(&self, klass: *mut c_void, name: &str, param_count: i32, param0_substr: &str) -> *mut c_void;
    fn class_get_parent(&self, klass: *mut c_void) -> *mut c_void;
    fn object_get_class(&self, obj: *mut c_void) -> *mut c_void;
    fn class_is_assignable_from(&self, parent: *mut c_void, child: *mut c_void) -> bool;
    fn class_get_type_object(&self, klass: *mut c_void) -> *mut c_void;
    fn class_init(&self, klass: *mut c_void);
    fn invoke(&self, method: *mut c_void, obj: *mut c_void, args: &mut [*mut c_void]) -> Option<*mut c_void>;
    fn unbox(&self, boxed: *mut c_void) -> *mut c_void;
    fn string_new(&self, s: &str) -> *mut c_void;
    fn string_to_rust(&self, s: *mut c_void) -> String;
    fn string_array_new(&self, items: &[&str]) -> *mut c_void;
    fn array_elements(&self, arr: *mut c_void) -> Vec<*mut c_void>;
    fn gchandle_pin(&self, obj: *mut c_void) -> usize;
    fn gchandle_free(&self, handle: usize);
    fn object_new(&self, klass: *mut c_void) -> *mut c_void;
    fn field_from_name(&self, klass: *mut c_void, name: &str) -> *mut c_void;
    fn field_get_value(&self, obj: *mut c_void, field: *mut c_void, out: *mut c_void);
    fn field_set_value(&self, obj: *mut c_void, field: *mut c_void, val: *mut c_void);
    fn field_static_set_value(&self, field: *mut c_void, val: *mut c_void);
    fn resolve_icall(&self, _name: &str) -> *mut c_void {
        std::ptr::null_mut()
    }

    fn find_method_in_hierarchy(&self, mut klass: *mut c_void, name: &str, param_count: i32) -> *mut c_void {
        while !klass.is_null() {
            let m = self.find_method(klass, name, param_count);
            if !m.is_null() {
                return m;
            }
            klass = self.class_get_parent(klass);
        }
        std::ptr::null_mut()
    }

    fn find_field_in_hierarchy(&self, mut klass: *mut c_void, name: &str) -> *mut c_void {
        while !klass.is_null() {
            let f = self.field_from_name(klass, name);
            if !f.is_null() {
                return f;
            }
            klass = self.class_get_parent(klass);
        }
        std::ptr::null_mut()
    }
}

struct AsyncUnityCache {
    unity_obj_resolved: bool,
    t_unity_obj: *mut c_void,
    f_cached_ptr: *mut c_void,
    m_op_implicit: *mut c_void,
    layout_meta_resolved: bool,
    t_graphic: *mut c_void,
    t_rect_transform: *mut c_void,
    t_canvas: *mut c_void,
    t_layout_group: *mut c_void,
    t_layout_fitter: *mut c_void,
    t_tmp_text: *mut c_void,
    m_tmp_force_mesh_0: *mut c_void,
    m_tmp_force_mesh_2: *mut c_void,
    m_graphic_get_rt: *mut c_void,
    m_graphic_set_layout_dirty: *mut c_void,
    m_transform_get_parent: *mut c_void,
    m_comp_get_component: *mut c_void,
    m_mark_layout_for_rebuild: *mut c_void,
    m_force_rebuild_layout: *mut c_void,
    setters: HashMap<(usize, u64), *mut c_void>,
}

unsafe impl Send for AsyncUnityCache {}

static ASYNC_UNITY_CACHE: LazyLock<Mutex<AsyncUnityCache>> = LazyLock::new(|| {
    Mutex::new(AsyncUnityCache {
        unity_obj_resolved: false,
        t_unity_obj: std::ptr::null_mut(),
        f_cached_ptr: std::ptr::null_mut(),
        m_op_implicit: std::ptr::null_mut(),
        layout_meta_resolved: false,
        t_graphic: std::ptr::null_mut(),
        t_rect_transform: std::ptr::null_mut(),
        t_canvas: std::ptr::null_mut(),
        t_layout_group: std::ptr::null_mut(),
        t_layout_fitter: std::ptr::null_mut(),
        t_tmp_text: std::ptr::null_mut(),
        m_tmp_force_mesh_0: std::ptr::null_mut(),
        m_tmp_force_mesh_2: std::ptr::null_mut(),
        m_graphic_get_rt: std::ptr::null_mut(),
        m_graphic_set_layout_dirty: std::ptr::null_mut(),
        m_transform_get_parent: std::ptr::null_mut(),
        m_comp_get_component: std::ptr::null_mut(),
        m_mark_layout_for_rebuild: std::ptr::null_mut(),
        m_force_rebuild_layout: std::ptr::null_mut(),
        setters: HashMap::new(),
    })
});

static FRAME_COUNTER: AtomicU32 = AtomicU32::new(0);

struct GcHandleGuard<'a> {
    rt: &'a dyn UnityRuntime,
    handle: usize,
}

impl Drop for GcHandleGuard<'_> {
    fn drop(&mut self) {
        if self.handle != 0 {
            self.rt.gchandle_free(self.handle);
        }
    }
}

fn ensure_unity_object_meta(rt: &dyn UnityRuntime, cache: &mut AsyncUnityCache) {
    if cache.unity_obj_resolved {
        return;
    }
    cache.unity_obj_resolved = true;
    let t_obj = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Object");
    cache.t_unity_obj = t_obj;
    if !t_obj.is_null() {
        cache.f_cached_ptr = rt.find_field_in_hierarchy(t_obj, "m_CachedPtr");
        if cache.f_cached_ptr.is_null() {
            cache.m_op_implicit = rt.find_method(t_obj, "op_Implicit", 1);
        }
    }
}

fn is_unity_object_alive(
    rt: &dyn UnityRuntime,
    cache: &mut AsyncUnityCache,
    comp: *mut c_void,
    comp_cls: *mut c_void,
) -> bool {
    if comp.is_null() || comp_cls.is_null() {
        return false;
    }
    ensure_unity_object_meta(rt, cache);
    if !cache.t_unity_obj.is_null() && !rt.class_is_assignable_from(cache.t_unity_obj, comp_cls) {
        return true;
    }
    if !cache.f_cached_ptr.is_null() {
        let mut ptr: *mut c_void = std::ptr::null_mut();
        rt.field_get_value(comp, cache.f_cached_ptr, &mut ptr as *mut *mut c_void as *mut c_void);
        return !ptr.is_null();
    }
    if !cache.m_op_implicit.is_null() {
        if let Some(boxed) = rt.invoke(cache.m_op_implicit, std::ptr::null_mut(), &mut [comp]) {
            let p = rt.unbox(boxed) as *const u8;
            return !p.is_null() && unsafe { *p } != 0;
        }
        return false;
    }
    false
}

fn resolve_setter_method(
    rt: &dyn UnityRuntime,
    cache: &mut AsyncUnityCache,
    comp_cls: *mut c_void,
    hook_id: u64,
) -> *mut c_void {
    let key = (comp_cls as usize, hook_id);
    if let Some(&m) = cache.setters.get(&key) {
        return m;
    }
    let primary = if hook_id == HOOK_UIE_TEXTFIELD_SET_VALUE {
        "set_value"
    } else {
        "set_text"
    };
    let mut m = rt.find_method_in_hierarchy(comp_cls, primary, 1);
    if m.is_null() && (is_tmp_hook(hook_id) || hook_id == HOOK_FEBUCCI_TEXTANIMATOR_SET_TEXT) {
        let mut cur = comp_cls;
        while !cur.is_null() {
            m = rt.find_method_param0(cur, "SetText", 1, "String");
            if !m.is_null() {
                break;
            }
            cur = rt.class_get_parent(cur);
        }
    }
    if !m.is_null() {
        if cache.setters.len() >= 256 {
            cache.setters.clear();
        }
        cache.setters.insert(key, m);
    }
    m
}

/// Writes masked/restored RTL typewriter text directly to a `TMP_Text` component
/// (invoked under `hooks::Enter` so our `set_text` detour passes straight through).
pub(crate) fn apply_tmp_max_visible_text(rt: &dyn UnityRuntime, comp: *mut c_void, text: &str) {
    if comp.is_null() {
        return;
    }
    let comp_cls = rt.object_get_class(comp);
    if comp_cls.is_null() {
        return;
    }
    let m_set = {
        let mut cache = ASYNC_UNITY_CACHE.lock().unwrap_or_else(|p| p.into_inner());
        resolve_setter_method(rt, &mut cache, comp_cls, HOOK_TMP_SET_TEXT)
    };
    if m_set.is_null() {
        return;
    }
    let new_str = rt.string_new(text);
    if !new_str.is_null() {
        let _ = rt.invoke(m_set, comp, &mut [new_str]);
    }
}

pub(crate) fn resolve_font_target(
    rt: &dyn UnityRuntime,
    comp: *mut c_void,
    hook_id: u64,
) -> (*mut c_void, u64) {
    if hook_id != HOOK_FEBUCCI_TEXTANIMATOR_SET_TEXT || comp.is_null() {
        return (comp, hook_id);
    }
    let comp_cls = rt.object_get_class(comp);
    if comp_cls.is_null() {
        return (std::ptr::null_mut(), hook_id);
    }
    let m_get = rt.find_method_in_hierarchy(comp_cls, "get_tmproText", 0);
    if let Some(tmpro) = rt.invoke(m_get, comp, &mut []) {
        if !tmpro.is_null() {
            return (tmpro, HOOK_TMP_SET_TEXT);
        }
    }
    (std::ptr::null_mut(), hook_id)
}

fn ensure_layout_meta(rt: &dyn UnityRuntime, cache: &mut AsyncUnityCache) {
    if cache.layout_meta_resolved {
        return;
    }
    cache.layout_meta_resolved = true;
    let t_graphic = rt.find_class("UnityEngine.UI", "UnityEngine.UI", "Graphic");
    cache.t_graphic = t_graphic;
    if !t_graphic.is_null() {
        cache.m_graphic_get_rt = rt.find_method_in_hierarchy(t_graphic, "get_rectTransform", 0);
        cache.m_graphic_set_layout_dirty = rt.find_method_in_hierarchy(t_graphic, "SetLayoutDirty", 0);
    }
    cache.t_rect_transform = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "RectTransform");
    cache.t_canvas = rt.find_class("UnityEngine.UIModule", "UnityEngine", "Canvas");

    let t_ilg = rt.find_class("UnityEngine.UI", "UnityEngine.UI", "ILayoutGroup");
    cache.t_layout_group = if !t_ilg.is_null() {
        t_ilg
    } else {
        rt.find_class("UnityEngine.UI", "UnityEngine.UI", "LayoutGroup")
    };
    let t_ils = rt.find_class("UnityEngine.UI", "UnityEngine.UI", "ILayoutSelfController");
    cache.t_layout_fitter = if !t_ils.is_null() {
        t_ils
    } else {
        rt.find_class("UnityEngine.UI", "UnityEngine.UI", "ContentSizeFitter")
    };

    let t_tr = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Transform");
    if !t_tr.is_null() {
        cache.m_transform_get_parent = rt.find_method_in_hierarchy(t_tr, "get_parent", 0);
    }
    let t_comp = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Component");
    if !t_comp.is_null() {
        cache.m_comp_get_component = rt.find_method_param0(t_comp, "GetComponent", 1, "Type");
    }

    let t_rebuilder = rt.find_class("UnityEngine.UI", "UnityEngine.UI", "LayoutRebuilder");
    if !t_rebuilder.is_null() {
        cache.m_mark_layout_for_rebuild =
            rt.find_method_param0(t_rebuilder, "MarkLayoutForRebuild", 1, "RectTransform");
        cache.m_force_rebuild_layout =
            rt.find_method_param0(t_rebuilder, "ForceRebuildLayoutImmediate", 1, "RectTransform");
    }

    let t_tmp = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Text");
    cache.t_tmp_text = t_tmp;
    if !t_tmp.is_null() {
        cache.m_tmp_force_mesh_0 = rt.find_method_in_hierarchy(t_tmp, "ForceMeshUpdate", 0);
        cache.m_tmp_force_mesh_2 = rt.find_method_in_hierarchy(t_tmp, "ForceMeshUpdate", 2);
    }
}

fn has_live_component(
    rt: &dyn UnityRuntime,
    cache: &mut AsyncUnityCache,
    target: *mut c_void,
    klass: *mut c_void,
) -> bool {
    if target.is_null() || klass.is_null() || cache.m_comp_get_component.is_null() {
        return false;
    }
    let type_obj = rt.class_get_type_object(klass);
    if type_obj.is_null() {
        return false;
    }
    let Some(found) = rt.invoke(cache.m_comp_get_component, target, &mut [type_obj]) else {
        return false;
    };
    if found.is_null() {
        return false;
    }
    let found_cls = rt.object_get_class(found);
    is_unity_object_alive(rt, cache, found, found_cls)
}

/// Refreshes TMP preferred-size metrics, marks the text and any ancestor layout
/// containers dirty (stopping before any Canvas), and returns the highest layout
/// root `RectTransform` to rebuild once per frame batch.
fn prepare_component_layout(
    rt: &dyn UnityRuntime,
    cache: &mut AsyncUnityCache,
    comp: *mut c_void,
    comp_cls: *mut c_void,
    hook_id: u64,
) -> Option<*mut c_void> {
    ensure_layout_meta(rt, cache);
    if cache.t_graphic.is_null() || !rt.class_is_assignable_from(cache.t_graphic, comp_cls) {
        return None;
    }

    // Update TMP preferredWidth/preferredHeight before layout containers measure it.
    if is_tmp_hook(hook_id) && !cache.t_tmp_text.is_null() && rt.class_is_assignable_from(cache.t_tmp_text, comp_cls) {
        if !cache.m_tmp_force_mesh_2.is_null() {
            let mut ignore_inactive: u8 = 0;
            let mut force_reparse: u8 = 0;
            let _ = rt.invoke(
                cache.m_tmp_force_mesh_2,
                comp,
                &mut [
                    &mut ignore_inactive as *mut u8 as *mut c_void,
                    &mut force_reparse as *mut u8 as *mut c_void,
                ],
            );
        } else if !cache.m_tmp_force_mesh_0.is_null() {
            let _ = rt.invoke(cache.m_tmp_force_mesh_0, comp, &mut []);
        }
    }

    if !cache.m_graphic_set_layout_dirty.is_null() {
        let _ = rt.invoke(cache.m_graphic_set_layout_dirty, comp, &mut []);
    }
    if cache.m_graphic_get_rt.is_null() {
        return None;
    }
    let text_rt = rt.invoke(cache.m_graphic_get_rt, comp, &mut [])?;
    if text_rt.is_null() {
        return None;
    }
    if !cache.m_mark_layout_for_rebuild.is_null() {
        let _ = rt.invoke(cache.m_mark_layout_for_rebuild, std::ptr::null_mut(), &mut [text_rt]);
    }

    let mut highest_root = text_rt;
    if !cache.m_transform_get_parent.is_null() && !cache.t_rect_transform.is_null() {
        let mut cur = text_rt;
        for _ in 0..6 {
            let Some(parent) = rt.invoke(cache.m_transform_get_parent, cur, &mut []) else {
                break;
            };
            if parent.is_null() {
                break;
            }
            let parent_cls = rt.object_get_class(parent);
            if parent_cls.is_null() || !rt.class_is_assignable_from(cache.t_rect_transform, parent_cls) {
                break;
            }
            // Never trigger a full-Canvas rebuild; stop strictly below any Canvas.
            if has_live_component(rt, cache, parent, cache.t_canvas) {
                break;
            }
            let has_layout = has_live_component(rt, cache, parent, cache.t_layout_group)
                || has_live_component(rt, cache, parent, cache.t_layout_fitter);
            if has_layout {
                if !cache.m_mark_layout_for_rebuild.is_null() {
                    let _ = rt.invoke(cache.m_mark_layout_for_rebuild, std::ptr::null_mut(), &mut [parent]);
                }
                highest_root = parent;
            }
            cur = parent;
        }
    }

    Some(highest_root)
}

fn rebuild_component_layout(
    rt: &dyn UnityRuntime,
    cache: &mut AsyncUnityCache,
    comp: *mut c_void,
    comp_cls: *mut c_void,
    hook_id: u64,
) {
    if let Some(root_rt) = prepare_component_layout(rt, cache, comp, comp_cls, hook_id) {
        if !cache.m_force_rebuild_layout.is_null() {
            let _ = rt.invoke(cache.m_force_rebuild_layout, std::ptr::null_mut(), &mut [root_rt]);
        }
    }
}

pub(crate) fn flush_async_updates(rt: &dyn UnityRuntime) {
    let (updates, handles_to_free) = embed::take_async_work();
    for h in handles_to_free {
        rt.gchandle_free(h);
    }
    if updates.is_empty() {
        return;
    }
    let mut cache = ASYNC_UNITY_CACHE.lock().unwrap_or_else(|p| p.into_inner());
    let mut roots_to_rebuild: Vec<*mut c_void> = Vec::new();
    for u in updates {
        let _pin_guard = GcHandleGuard { rt, handle: u.gc_handle };
        let comp = u.component as *mut c_void;
        let comp_cls = rt.object_get_class(comp);
        if !is_unity_object_alive(rt, &mut cache, comp, comp_cls) {
            continue;
        }
        let m_set = resolve_setter_method(rt, &mut cache, comp_cls, u.hook_id);
        if m_set.is_null() {
            continue;
        }
        let (font_target, font_hook_id) = resolve_font_target(rt, comp, u.hook_id);
        if u.apply_font {
            embed::remember_output(&u.text);
            if !font_target.is_null() {
                font::on_text(rt, font_target, font_hook_id, &u.text);
            }
        }
        let new_str = rt.string_new(&u.text);
        if !new_str.is_null() {
            let _ = rt.invoke(m_set, comp, &mut [new_str]);
            let layout_comp = if font_target.is_null() { comp } else { font_target };
            let layout_cls = if layout_comp == comp {
                comp_cls
            } else {
                rt.object_get_class(layout_comp)
            };
            if !layout_cls.is_null() {
                if let Some(root_rt) =
                    prepare_component_layout(rt, &mut cache, layout_comp, layout_cls, font_hook_id)
                {
                    if !roots_to_rebuild.contains(&root_rt) {
                        roots_to_rebuild.push(root_rt);
                    }
                }
            }
        }
    }
    if !cache.m_force_rebuild_layout.is_null() {
        for root_rt in roots_to_rebuild {
            let _ = rt.invoke(cache.m_force_rebuild_layout, std::ptr::null_mut(), &mut [root_rt]);
        }
    }
}

fn find_active_components(rt: &dyn UnityRuntime, asm: &str, ns: &str, cls_name: &str) -> Vec<*mut c_void> {
    let t_obj = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Object");
    if t_obj.is_null() {
        return Vec::new();
    }
    let m_find_old = rt.find_method_param0(t_obj, "FindObjectsOfType", 1, "Type");
    let m_find_new = if m_find_old.is_null() {
        rt.find_method_param0(t_obj, "FindObjectsByType", 2, "Type")
    } else {
        std::ptr::null_mut()
    };
    if m_find_old.is_null() && m_find_new.is_null() {
        return Vec::new();
    }
    let t_behaviour = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Behaviour");
    let m_is_active = if t_behaviour.is_null() {
        std::ptr::null_mut()
    } else {
        rt.find_method(t_behaviour, "get_isActiveAndEnabled", 0)
    };
    let k = rt.find_class(asm, ns, cls_name);
    if k.is_null() {
        return Vec::new();
    }
    let type_obj = rt.class_get_type_object(k);
    if type_obj.is_null() {
        return Vec::new();
    }
    let arr = if !m_find_old.is_null() {
        rt.invoke(m_find_old, std::ptr::null_mut(), &mut [type_obj])
    } else {
        let mut sort_mode: i32 = 0; // FindObjectsSortMode.None
        rt.invoke(
            m_find_new,
            std::ptr::null_mut(),
            &mut [type_obj, &mut sort_mode as *mut i32 as *mut c_void],
        )
    };
    let Some(arr) = arr else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for comp in rt.array_elements(arr).into_iter().take(256) {
        if comp.is_null() {
            continue;
        }
        if !m_is_active.is_null() {
            let Some(boxed) = rt.invoke(m_is_active, comp, &mut []) else { continue };
            let ptr = rt.unbox(boxed) as *const u8;
            if ptr.is_null() || unsafe { *ptr } == 0 {
                continue;
            }
        }
        out.push(comp);
    }
    out
}

fn scan_active_static_ui(rt: &dyn UnityRuntime) {
    let targets = [
        ("Unity.TextMeshPro", "TMPro", "TMP_Text", HOOK_TMP_STATIC_UI),
        ("UnityEngine.UI", "UnityEngine.UI", "Text", HOOK_UI_TEXT_STATIC_UI),
    ];
    for (asm, ns, cls_name, hook_id) in targets {
        for comp in find_active_components(rt, asm, ns, cls_name) {
            handle_static_ui_on_enable(rt, comp, hook_id);
        }
    }
}

fn reapply_single_component(rt: &dyn UnityRuntime, comp: *mut c_void, hook_id: u64, ctx: u64, ctx2: u64) {
    let comp_cls = rt.object_get_class(comp);
    if comp_cls.is_null() {
        return;
    }
    let text_ptr = read_component_text(rt, comp, comp_cls, hook_id);
    if text_ptr.is_null() {
        return;
    }
    let raw = rt.string_to_rust(text_ptr);
    if raw.trim().is_empty() {
        return;
    }
    if !embed::clear_text_enabled() && !embed::has_translatable_letters(&raw) {
        return;
    }
    let cleaned: Cow<'_, str> = if is_tmp_like_tags(hook_id) {
        clean_tmp_tags(&raw)
    } else {
        Cow::Borrowed(raw.as_str())
    };
    if let embed::ProcessOutcome::Replace { text: replaced, apply_font } = embed::process_unity(
        hook_id,
        || ctx,
        ctx2,
        comp as usize,
        &cleaned,
        &raw,
        || rt.gchandle_pin(comp),
    ) {
        let mut cache = ASYNC_UNITY_CACHE.lock().unwrap_or_else(|p| p.into_inner());
        let m_set = resolve_setter_method(rt, &mut cache, comp_cls, hook_id);
        if m_set.is_null() {
            return;
        }
        if apply_font {
            embed::remember_output(&replaced);
            let (font_target, font_hook_id) = resolve_font_target(rt, comp, hook_id);
            if !font_target.is_null() {
                font::on_text(rt, font_target, font_hook_id, &replaced);
            }
        }
        let new_str = rt.string_new(&replaced);
        if !new_str.is_null() {
            let _ = rt.invoke(m_set, comp, &mut [new_str]);
            rebuild_component_layout(rt, &mut cache, comp, comp_cls, hook_id);
        }
    }
}

/// Immediately translates currently visible Unity components for hooks that were just enabled.
fn reapply_enabled_hooks(rt: &dyn UnityRuntime) {
    let targets = embed::take_reapply_targets();
    if targets.is_empty() {
        return;
    }
    let need_static_tmp = targets.iter().any(|&(h, _, _)| h == HOOK_TMP_STATIC_UI);
    let need_static_ui = targets.iter().any(|&(h, _, _)| h == HOOK_UI_TEXT_STATIC_UI);
    let need_dyn_tmp = targets.iter().any(|&(h, _, _)| is_tmp_hook(h) && h != HOOK_TMP_STATIC_UI);
    let need_dyn_ui = targets.iter().any(|&(h, _, _)| h == HOOK_UI_TEXT_SET_TEXT);

    if need_static_tmp || need_dyn_tmp {
        for comp in find_active_components(rt, "Unity.TextMeshPro", "TMPro", "TMP_Text") {
            if need_dyn_tmp {
                if let Some((hid, ctx, ctx2)) = embed::find_reapply_hook_for_component(comp as usize, &targets) {
                    reapply_single_component(rt, comp, hid, ctx, ctx2);
                    continue;
                }
            }
            if need_static_tmp && embed::verbose_hooks_enabled() {
                handle_static_ui_on_enable(rt, comp, HOOK_TMP_STATIC_UI);
            }
        }
    }

    if need_static_ui || need_dyn_ui {
        for comp in find_active_components(rt, "UnityEngine.UI", "UnityEngine.UI", "Text") {
            if need_dyn_ui {
                if let Some((hid, ctx, ctx2)) = embed::find_reapply_hook_for_component(comp as usize, &targets) {
                    reapply_single_component(rt, comp, hid, ctx, ctx2);
                    continue;
                }
            }
            if need_static_ui && embed::verbose_hooks_enabled() {
                handle_static_ui_on_enable(rt, comp, HOOK_UI_TEXT_STATIC_UI);
            }
        }
    }
}

pub(crate) fn on_frame_tick(rt: &dyn UnityRuntime) {
    if embed::has_async_in_flight()
        && FRAME_COUNTER.fetch_add(1, Ordering::Relaxed).is_multiple_of(60)
    {
        embed::reap_expired_async();
    }
    if embed::has_debounce_work() {
        embed::flush_ready_debounced();
    }
    if embed::take_verbose_scan_pending() && embed::verbose_hooks_enabled() {
        scan_active_static_ui(rt);
    }
    if embed::has_reapply_pending() {
        reapply_enabled_hooks(rt);
    }
    if embed::has_async_work() {
        flush_async_updates(rt);
    }
}

fn read_component_text(rt: &dyn UnityRuntime, comp: *mut c_void, comp_cls: *mut c_void, hook_id: u64) -> *mut c_void {
    let field_name = if is_ui_text_hook(hook_id) { "m_Text" } else { "m_text" };
    let f_text = rt.find_field_in_hierarchy(comp_cls, field_name);
    if !f_text.is_null() {
        let mut ptr: *mut c_void = std::ptr::null_mut();
        rt.field_get_value(comp, f_text, &mut ptr as *mut *mut c_void as *mut c_void);
        if !ptr.is_null() {
            return ptr;
        }
    }
    let m_get = rt.find_method_in_hierarchy(comp_cls, "get_text", 0);
    rt.invoke(m_get, comp, &mut []).unwrap_or(std::ptr::null_mut())
}

/// Captures static/prefab text from `OnEnable` when `verbose_hooks` is enabled.
pub(crate) fn handle_static_ui_on_enable(rt: &dyn UnityRuntime, component: *mut c_void, hook_id: u64) {
    if component.is_null() || !embed::verbose_hooks_enabled() {
        return;
    }
    let comp_cls = rt.object_get_class(component);
    if comp_cls.is_null() {
        return;
    }
    let text_ptr = read_component_text(rt, component, comp_cls, hook_id);
    if text_ptr.is_null() {
        return;
    }
    let raw = rt.string_to_rust(text_ptr);
    if raw.trim().is_empty() {
        return;
    }
    if !embed::clear_text_enabled() && !embed::has_translatable_letters(&raw) {
        return;
    }
    let cleaned: Cow<'_, str> = if is_tmp_hook(hook_id) {
        clean_tmp_tags(&raw)
    } else {
        Cow::Borrowed(raw.as_str())
    };
    if let embed::ProcessOutcome::Replace { text: replaced, apply_font } =
        embed::process_unity(hook_id, || 0, 0, component as usize, &cleaned, &raw, || rt.gchandle_pin(component))
    {
        let m_set = {
            let mut cache = ASYNC_UNITY_CACHE.lock().unwrap_or_else(|p| p.into_inner());
            resolve_setter_method(rt, &mut cache, comp_cls, hook_id)
        };
        if m_set.is_null() {
            return;
        }
        if apply_font {
            embed::remember_output(&replaced);
            font::on_text(rt, component, hook_id, &replaced);
        }
        let new_str = rt.string_new(&replaced);
        if !new_str.is_null() {
            let _ = rt.invoke(m_set, component, &mut [new_str]);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn clean_tmp_tags_borrows_plain_and_strips_tags() {
        let _guard = embed::test_guard();
        assert!(matches!(clean_tmp_tags("Hello world 123"), Cow::Borrowed("Hello world 123")));
        assert!(matches!(clean_tmp_tags("<color=red>Hi</color>"), Cow::Borrowed("<color=red>Hi</color>")));

        assert_eq!(clean_tmp_tags("<NoParse>HP</noparse>").as_ref(), "HP");
        assert_eq!(
            clean_tmp_tags("<line-height=120%>Hello<sprite anim=0,1,2>").as_ref(),
            "Hello"
        );

        let prev = embed::settings().strip_typewriter_tags;
        let mut s = (*embed::settings()).clone();
        s.strip_typewriter_tags = true;
        embed::set_settings(s);
        assert_eq!(
            clean_tmp_tags("A<COLOR=#00000000>B<color=red>C</color></color>D").as_ref(),
            "AB<color=red>C</color>D"
        );
        let mut restore = (*embed::settings()).clone();
        restore.strip_typewriter_tags = prev;
        embed::set_settings(restore);
    }

    #[test]
    fn utf16_prefilter_filters_non_translatable() {
        let u = |s: &str| -> Vec<u16> { s.encode_utf16().collect() };
        assert!(!utf16_has_translatable_letters(&u("")));
        assert!(!utf16_has_translatable_letters(&u("A")));
        assert!(!utf16_has_translatable_letters(&u("12345 / 60")));
        assert!(!utf16_has_translatable_letters(&u("... --- !!!")));
        assert!(!utf16_has_translatable_letters(&u("<color=red>99%</color>")));
        assert!(!utf16_has_translatable_letters(&u("مرحبا بالعالم \u{FE8E}\u{FE92}")));
        assert!(!utf16_has_translatable_letters(&u("Hello World\n\u{FE8E}\u{FE92}")));
        assert!(!utf16_has_translatable_letters(&u("\u{FE8E}\u{FE92}\nHello World")));
        assert!(utf16_has_translatable_letters(&u("Health < 50 HP")));
        assert!(utf16_has_translatable_letters(&u("<color=red>HP:</color> 50")));
        assert!(utf16_has_translatable_letters(&u("Start Game")));
        assert!(utf16_has_translatable_letters(&u("日本語テスト")));
    }
}
