//! Unity runtime detection, text hooking (Mono & IL2CPP), and font injection.

pub mod font;
pub mod il2cpp;
pub mod mono;

use std::ffi::{c_char, c_void, CString};
use std::sync::OnceLock;
use std::time::{Duration, Instant};

use khutaf_proto::level;
use windows_sys::Win32::Foundation::HMODULE;
use windows_sys::Win32::System::LibraryLoader::{GetModuleHandleW, GetProcAddress};
use windows_sys::Win32::System::Memory::{VirtualQuery, MEMORY_BASIC_INFORMATION};

use crate::{embed, hooks};

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

pub(crate) fn is_tmp_hook(hook_id: u64) -> bool {
    matches!(
        hook_id,
        HOOK_TMP_SET_TEXT
            | HOOK_TMP_SETTEXT
            | HOOK_TEXTMESHPRO_SET_TEXT
            | HOOK_TEXTMESHPRO_UGUI_SET_TEXT
            | HOOK_TEXTMESHPRO_UGUI_SETTEXT
    )
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
            continue;
        }
        if let Some(lookup_fn) = lookup {
            let mut image_base: u64 = 0;
            let rf = unsafe { lookup_fn(frame as u64, &mut image_base, std::ptr::null_mut()) };
            if !rf.is_null() && image_base != 0 && image_base != self_base as u64 {
                return unsafe { (*rf).begin_addr as u64 };
            }
        }
        return (frame as usize).wrapping_sub(alloc_base) as u64;
    }
    0
}

fn strip_tag_prefix(s: &str, prefix: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut rest = s;
    while let Some(start) = rest.find(prefix) {
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
    out
}

/// Strips non-semantic TextMeshPro layout tags (`<noparse>`, `<line-height=...>`, `<sprite anim=...>`).
pub(crate) fn clean_tmp_tags(s: &str) -> String {
    let s = s.replace("<noparse>", "").replace("</noparse>", "");
    let s = strip_tag_prefix(&s, "<line-height=");
    strip_tag_prefix(&s, "<sprite anim=")
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

pub fn install(mut engine: Engine) {
    if matches!(engine, Engine::Unknown) {
        crate::log(level::WARN, "unknown engine; only Unity Mono/IL2CPP is supported");
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
        if let Err(e) = unsafe { hooks::enable_all() } {
            crate::log(level::ERROR, format!("enable hooks failed: {e}"));
        } else {
            crate::log(level::INFO, format!("installed {count} {} text hook(s)", engine.name()));
        }
    } else {
        crate::log(level::WARN, format!("no text hooks found for {}", engine.name()));
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
    fn gchandle_pin(&self, obj: *mut c_void);
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
