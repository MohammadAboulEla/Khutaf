//! Unity Mono C-API resolver and text setter detours.

use std::borrow::Cow;
use std::collections::HashSet;
use std::ffi::{c_char, c_void, CString};
use std::sync::atomic::{AtomicPtr, Ordering};

use khutaf_proto::level;
use windows_sys::Win32::Foundation::HMODULE;

use super::{
    caller_rel_ctx, clean_tmp_tags, cstr_to_string, font, is_tmp_hook, module_handle, proc_addr,
    UnityRuntime, HOOK_GUITEXT_SET_TEXT, HOOK_NGUILABEL_SET_TEXT, HOOK_TEXTMESHPRO_SET_TEXT,
    HOOK_TEXTMESHPRO_UGUI_SETTEXT, HOOK_TEXTMESHPRO_UGUI_SET_TEXT, HOOK_TEXTMESH_SET_TEXT,
    HOOK_TMP_SETTEXT, HOOK_TMP_SET_TEXT, HOOK_UIE_TEXTELEMENT_SET_TEXT,
    HOOK_UIE_TEXTFIELD_SET_VALUE, HOOK_UI_TEXT_SET_TEXT,
};
use crate::{embed, hooks};

type MonoStringReadChars = unsafe extern "C" fn(*mut c_void) -> *const u16;
type MonoStringReadLen = unsafe extern "C" fn(*mut c_void) -> i32;

pub(crate) struct MonoApi {
    get_root_domain: unsafe extern "C" fn() -> *mut c_void,
    thread_attach: unsafe extern "C" fn(*mut c_void) -> *mut c_void,
    thread_detach: Option<unsafe extern "C" fn(*mut c_void)>,
    assembly_foreach: unsafe extern "C" fn(unsafe extern "C" fn(*mut c_void, *mut c_void), *mut c_void),
    assembly_get_image: unsafe extern "C" fn(*mut c_void) -> *mut c_void,
    image_get_name: Option<unsafe extern "C" fn(*mut c_void) -> *const c_char>,
    class_from_name: unsafe extern "C" fn(*mut c_void, *const c_char, *const c_char) -> *mut c_void,
    class_get_method_from_name: unsafe extern "C" fn(*mut c_void, *const c_char, i32) -> *mut c_void,
    class_get_methods: Option<unsafe extern "C" fn(*mut c_void, *mut *mut c_void) -> *mut c_void>,
    method_get_name: Option<unsafe extern "C" fn(*mut c_void) -> *const c_char>,
    method_signature: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    signature_get_param_count: Option<unsafe extern "C" fn(*mut c_void) -> u32>,
    signature_get_params: Option<unsafe extern "C" fn(*mut c_void, *mut *mut c_void) -> *mut c_void>,
    class_from_mono_type: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    class_get_name: Option<unsafe extern "C" fn(*mut c_void) -> *const c_char>,
    class_get_parent: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    class_is_assignable_from: Option<unsafe extern "C" fn(*mut c_void, *mut c_void) -> i32>,
    class_get_type: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    type_get_object: Option<unsafe extern "C" fn(*mut c_void, *mut c_void) -> *mut c_void>,
    object_get_class: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    compile_method: unsafe extern "C" fn(*mut c_void) -> *mut c_void,
    runtime_invoke: Option<
        unsafe extern "C" fn(*mut c_void, *mut c_void, *mut *mut c_void, *mut *mut c_void) -> *mut c_void,
    >,
    object_unbox: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    string_chars: Option<MonoStringReadChars>,
    string_length: Option<MonoStringReadLen>,
    string_new_utf16: unsafe extern "C" fn(*mut c_void, *const u16, i32) -> *mut c_void,
    get_string_class: Option<unsafe extern "C" fn() -> *mut c_void>,
    array_new: Option<unsafe extern "C" fn(*mut c_void, *mut c_void, u32) -> *mut c_void>,
    array_length: Option<unsafe extern "C" fn(*mut c_void) -> u32>,
    array_addr_with_size: Option<unsafe extern "C" fn(*mut c_void, i32, usize) -> *mut u8>,
    gchandle_new: Option<unsafe extern "C" fn(*mut c_void, i32) -> u32>,
    gchandle_free: Option<unsafe extern "C" fn(u32)>,
    object_new: Option<unsafe extern "C" fn(*mut c_void, *mut c_void) -> *mut c_void>,
    class_get_field_from_name: Option<unsafe extern "C" fn(*mut c_void, *const c_char) -> *mut c_void>,
    field_get_value: Option<unsafe extern "C" fn(*mut c_void, *mut c_void, *mut c_void)>,
    field_set_value: Option<unsafe extern "C" fn(*mut c_void, *mut c_void, *mut c_void)>,
    field_get_parent: Option<unsafe extern "C" fn(*mut c_void) -> *mut c_void>,
    type_get_name: Option<unsafe extern "C" fn(*mut c_void) -> *const c_char>,
    class_vtable: Option<unsafe extern "C" fn(*mut c_void, *mut c_void) -> *mut c_void>,
    field_static_set_value: Option<unsafe extern "C" fn(*mut c_void, *mut c_void, *mut c_void)>,
    runtime_class_init: Option<unsafe extern "C" fn(*mut c_void)>,
}

static MONO: AtomicPtr<MonoApi> = AtomicPtr::new(std::ptr::null_mut());
static MONO_INIT_MU: std::sync::Mutex<()> = std::sync::Mutex::new(());

static ORIG_TMP_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TMP_SETTEXT_1: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TMP_SETTEXT_2: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_UI_TEXT_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TEXTMESH_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TEXTMESHPRO_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TEXTMESHPRO_UGUI_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TEXTMESHPRO_UGUI_SETTEXT_1: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_TEXTMESHPRO_UGUI_SETTEXT_2: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_UIE_TEXTELEMENT_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_UIE_TEXTFIELD_SET_VALUE: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_GUITEXT_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_NGUILABEL_SET_TEXT: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_CANVAS_WILL_RENDER: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());
static ORIG_CAMERA_PRE_CULL: AtomicPtr<c_void> = AtomicPtr::new(std::ptr::null_mut());

fn find_mono_module() -> HMODULE {
    for name in ["mono-2.0-bdwgc.dll", "mono-2.0-sgen.dll", "mono.dll"] {
        let h = module_handle(name);
        if !h.is_null() {
            return h;
        }
    }
    std::ptr::null_mut()
}

impl MonoApi {
    pub(crate) fn get() -> Option<&'static MonoApi> {
        let p = MONO.load(Ordering::Acquire);
        if !p.is_null() {
            return Some(unsafe { &*p });
        }
        let _guard = MONO_INIT_MU.lock().unwrap_or_else(|e| e.into_inner());
        let p = MONO.load(Ordering::Acquire);
        if !p.is_null() {
            return Some(unsafe { &*p });
        }
        let api = Self::load()?;
        let leaked = Box::into_raw(Box::new(api));
        MONO.store(leaked, Ordering::Release);
        Some(unsafe { &*leaked })
    }

    fn load() -> Option<MonoApi> {
        let m = find_mono_module();
        if m.is_null() {
            return None;
        }
        Some(MonoApi {
            get_root_domain: proc_addr(m, "mono_get_root_domain")?,
            thread_attach: proc_addr(m, "mono_thread_attach")?,
            thread_detach: proc_addr(m, "mono_thread_detach"),
            assembly_foreach: proc_addr(m, "mono_assembly_foreach")?,
            assembly_get_image: proc_addr(m, "mono_assembly_get_image")?,
            image_get_name: proc_addr(m, "mono_image_get_name"),
            class_from_name: proc_addr(m, "mono_class_from_name")?,
            class_get_method_from_name: proc_addr(m, "mono_class_get_method_from_name")?,
            class_get_methods: proc_addr(m, "mono_class_get_methods"),
            method_get_name: proc_addr(m, "mono_method_get_name"),
            method_signature: proc_addr(m, "mono_method_signature"),
            signature_get_param_count: proc_addr(m, "mono_signature_get_param_count"),
            signature_get_params: proc_addr(m, "mono_signature_get_params"),
            class_from_mono_type: proc_addr(m, "mono_class_from_mono_type"),
            class_get_name: proc_addr(m, "mono_class_get_name"),
            class_get_parent: proc_addr(m, "mono_class_get_parent"),
            class_is_assignable_from: proc_addr(m, "mono_class_is_assignable_from"),
            class_get_type: proc_addr(m, "mono_class_get_type"),
            type_get_object: proc_addr(m, "mono_type_get_object"),
            object_get_class: proc_addr(m, "mono_object_get_class"),
            compile_method: proc_addr(m, "mono_compile_method")?,
            runtime_invoke: proc_addr(m, "mono_runtime_invoke"),
            object_unbox: proc_addr(m, "mono_object_unbox"),
            string_chars: proc_addr(m, "mono_string_chars"),
            string_length: proc_addr(m, "mono_string_length"),
            string_new_utf16: proc_addr(m, "mono_string_new_utf16")?,
            get_string_class: proc_addr(m, "mono_get_string_class"),
            array_new: proc_addr(m, "mono_array_new"),
            array_length: proc_addr(m, "mono_array_length"),
            array_addr_with_size: proc_addr(m, "mono_array_addr_with_size"),
            gchandle_new: proc_addr(m, "mono_gchandle_new"),
            gchandle_free: proc_addr(m, "mono_gchandle_free"),
            object_new: proc_addr(m, "mono_object_new"),
            class_get_field_from_name: proc_addr(m, "mono_class_get_field_from_name"),
            field_get_value: proc_addr(m, "mono_field_get_value"),
            field_set_value: proc_addr(m, "mono_field_set_value"),
            field_get_parent: proc_addr(m, "mono_field_get_parent"),
            type_get_name: proc_addr(m, "mono_type_get_name"),
            class_vtable: proc_addr(m, "mono_class_vtable"),
            field_static_set_value: proc_addr(m, "mono_field_static_set_value"),
            runtime_class_init: proc_addr(m, "mono_runtime_class_init"),
        })
    }

    fn root_domain(&self) -> *mut c_void {
        unsafe { (self.get_root_domain)() }
    }

    fn attach_thread(&self) -> *mut c_void {
        let dom = self.root_domain();
        if dom.is_null() {
            return std::ptr::null_mut();
        }
        unsafe { (self.thread_attach)(dom) }
    }

    fn images(&self) -> Vec<*mut c_void> {
        struct Ctx {
            get_image: unsafe extern "C" fn(*mut c_void) -> *mut c_void,
            out: Vec<*mut c_void>,
        }
        unsafe extern "C" fn cb(assembly: *mut c_void, data: *mut c_void) {
            let ctx = &mut *(data as *mut Ctx);
            let img = (ctx.get_image)(assembly);
            if !img.is_null() {
                ctx.out.push(img);
            }
        }
        let mut ctx = Ctx { get_image: self.assembly_get_image, out: Vec::new() };
        unsafe { (self.assembly_foreach)(cb, &mut ctx as *mut Ctx as *mut c_void) };
        ctx.out
    }

    fn find_method_typed(&self, klass: *mut c_void, name: &str, param_names: &[&str]) -> *mut c_void {
        let (Some(get_methods), Some(get_name), Some(get_sig), Some(param_cnt), Some(get_params)) = (
            self.class_get_methods,
            self.method_get_name,
            self.method_signature,
            self.signature_get_param_count,
            self.signature_get_params,
        ) else {
            return std::ptr::null_mut();
        };

        let mut iter: *mut c_void = std::ptr::null_mut();
        loop {
            let m = unsafe { get_methods(klass, &mut iter) };
            if m.is_null() {
                break;
            }
            if cstr_to_string(unsafe { get_name(m) }) != name {
                continue;
            }
            let sig = unsafe { get_sig(m) };
            if sig.is_null() {
                continue;
            }
            if unsafe { param_cnt(sig) } as usize != param_names.len() {
                continue;
            }
            let mut p_iter: *mut c_void = std::ptr::null_mut();
            let mut matches = true;
            for expected in param_names {
                let ty = unsafe { get_params(sig, &mut p_iter) };
                let p_name = self.mono_type_class_name(ty);
                if !p_name.contains(expected) || (!expected.ends_with("[]") && p_name.ends_with("[]")) {
                    matches = false;
                    break;
                }
            }
            if matches {
                return m;
            }
        }
        std::ptr::null_mut()
    }

    fn mono_type_class_name(&self, ty: *mut c_void) -> String {
        if ty.is_null() {
            return String::new();
        }
        if let Some(type_name_fn) = self.type_get_name {
            let name = cstr_to_string(unsafe { type_name_fn(ty) });
            if !name.is_empty() {
                return name;
            }
        }
        let (Some(from_type), Some(cls_name)) = (self.class_from_mono_type, self.class_get_name) else {
            return String::new();
        };
        let k = unsafe { from_type(ty) };
        if k.is_null() {
            return String::new();
        }
        cstr_to_string(unsafe { cls_name(k) })
    }
    fn string_utf16_slice(&self, s: *mut c_void) -> Option<&[u16]> {
        if s.is_null() {
            return None;
        }
        let (ptr, len) = match (self.string_chars, self.string_length) {
            (Some(chars_fn), Some(len_fn)) => unsafe { (chars_fn(s), len_fn(s)) },
            _ => unsafe {
                let base = (s as *const u8).add(2 * std::mem::size_of::<usize>());
                let l = *(base as *const i32);
                let c = base.add(4) as *const u16;
                (c, l)
            },
        };
        if ptr.is_null() || len <= 0 || len > 1_000_000 {
            return None;
        }
        Some(unsafe { std::slice::from_raw_parts(ptr, len as usize) })
    }
}

impl UnityRuntime for MonoApi {
    fn find_class(&self, preferred_asm: &str, ns: &str, name: &str) -> *mut c_void {
        let Ok(c_ns) = CString::new(ns) else { return std::ptr::null_mut() };
        let Ok(c_name) = CString::new(name) else { return std::ptr::null_mut() };
        let mut fallback: *mut c_void = std::ptr::null_mut();
        for img in self.images() {
            let k = unsafe { (self.class_from_name)(img, c_ns.as_ptr(), c_name.as_ptr()) };
            if k.is_null() {
                continue;
            }
            if !preferred_asm.is_empty() {
                if let Some(img_name_fn) = self.image_get_name {
                    let img_name = cstr_to_string(unsafe { img_name_fn(img) });
                    if img_name.eq_ignore_ascii_case(preferred_asm)
                        || img_name.trim_end_matches(".dll").eq_ignore_ascii_case(preferred_asm)
                    {
                        return k;
                    }
                }
            }
            if fallback.is_null() {
                fallback = k;
            }
        }
        fallback
    }

    fn find_method(&self, klass: *mut c_void, name: &str, param_count: i32) -> *mut c_void {
        if klass.is_null() {
            return std::ptr::null_mut();
        }
        let Ok(c_name) = CString::new(name) else { return std::ptr::null_mut() };
        unsafe { (self.class_get_method_from_name)(klass, c_name.as_ptr(), param_count) }
    }

    fn find_method_param0(&self, klass: *mut c_void, name: &str, param_count: i32, param0_substr: &str) -> *mut c_void {
        let (Some(get_methods), Some(get_name), Some(get_sig), Some(param_cnt), Some(get_params)) = (
            self.class_get_methods,
            self.method_get_name,
            self.method_signature,
            self.signature_get_param_count,
            self.signature_get_params,
        ) else {
            return std::ptr::null_mut();
        };
        let mut iter: *mut c_void = std::ptr::null_mut();
        loop {
            let m = unsafe { get_methods(klass, &mut iter) };
            if m.is_null() {
                break;
            }
            if cstr_to_string(unsafe { get_name(m) }) != name {
                continue;
            }
            let sig = unsafe { get_sig(m) };
            if sig.is_null() || unsafe { param_cnt(sig) } != param_count as u32 {
                continue;
            }
            let mut p_iter: *mut c_void = std::ptr::null_mut();
            let p0 = unsafe { get_params(sig, &mut p_iter) };
            let pn = self.mono_type_class_name(p0);
            if pn.contains(param0_substr) && (param0_substr.ends_with("[]") || !pn.ends_with("[]")) {
                return m;
            }
        }
        std::ptr::null_mut()
    }

    fn class_get_parent(&self, klass: *mut c_void) -> *mut c_void {
        match self.class_get_parent {
            Some(f) if !klass.is_null() => unsafe { f(klass) },
            _ => std::ptr::null_mut(),
        }
    }

    fn object_get_class(&self, obj: *mut c_void) -> *mut c_void {
        if obj.is_null() {
            return std::ptr::null_mut();
        }
        if let Some(f) = self.object_get_class {
            return unsafe { f(obj) };
        }
        std::ptr::null_mut()
    }

    fn class_is_assignable_from(&self, parent: *mut c_void, child: *mut c_void) -> bool {
        if parent.is_null() || child.is_null() {
            return false;
        }
        if parent == child {
            return true;
        }
        match self.class_is_assignable_from {
            Some(f) => unsafe { f(parent, child) != 0 },
            None => false,
        }
    }

    fn class_get_type_object(&self, klass: *mut c_void) -> *mut c_void {
        if klass.is_null() {
            return std::ptr::null_mut();
        }
        let (Some(get_ty), Some(get_obj)) = (self.class_get_type, self.type_get_object) else {
            return std::ptr::null_mut();
        };
        let dom = self.root_domain();
        let ty = unsafe { get_ty(klass) };
        if dom.is_null() || ty.is_null() {
            return std::ptr::null_mut();
        }
        unsafe { get_obj(dom, ty) }
    }

    fn class_init(&self, klass: *mut c_void) {
        if let (Some(vtable_fn), Some(init_fn)) = (self.class_vtable, self.runtime_class_init) {
            let dom = self.root_domain();
            if !dom.is_null() && !klass.is_null() {
                let vt = unsafe { vtable_fn(dom, klass) };
                if !vt.is_null() {
                    unsafe { init_fn(vt) };
                }
            }
        }
    }

    fn invoke(&self, method: *mut c_void, obj: *mut c_void, args: &mut [*mut c_void]) -> Option<*mut c_void> {
        let invoke_fn = self.runtime_invoke?;
        if method.is_null() {
            return None;
        }
        let mut exc: *mut c_void = std::ptr::null_mut();
        let params = if args.is_empty() { std::ptr::null_mut() } else { args.as_mut_ptr() };
        let res = unsafe { invoke_fn(method, obj, params, &mut exc) };
        if !exc.is_null() {
            return None;
        }
        Some(res)
    }

    fn unbox(&self, boxed: *mut c_void) -> *mut c_void {
        if boxed.is_null() {
            return std::ptr::null_mut();
        }
        match self.object_unbox {
            Some(f) => unsafe { f(boxed) },
            None => unsafe { (boxed as *mut u8).add(2 * std::mem::size_of::<usize>()) as *mut c_void },
        }
    }

    fn string_new(&self, s: &str) -> *mut c_void {
        let dom = self.root_domain();
        if dom.is_null() {
            return std::ptr::null_mut();
        }
        let units: Vec<u16> = s.encode_utf16().collect();
        unsafe { (self.string_new_utf16)(dom, units.as_ptr(), units.len() as i32) }
    }

    fn string_to_rust(&self, s: *mut c_void) -> String {
        match self.string_utf16_slice(s) {
            Some(slice) => String::from_utf16_lossy(slice),
            None => String::new(),
        }
    }

    fn string_array_new(&self, items: &[&str]) -> *mut c_void {
        let (Some(arr_new), Some(str_cls_fn)) = (self.array_new, self.get_string_class) else {
            return std::ptr::null_mut();
        };
        let dom = self.root_domain();
        let str_cls = unsafe { str_cls_fn() };
        if dom.is_null() || str_cls.is_null() {
            return std::ptr::null_mut();
        }
        let arr = unsafe { arr_new(dom, str_cls, items.len() as u32) };
        if arr.is_null() {
            return std::ptr::null_mut();
        }
        for (i, item) in items.iter().enumerate() {
            let s = self.string_new(item);
            let slot = match self.array_addr_with_size {
                Some(f) => unsafe { f(arr, std::mem::size_of::<*mut c_void>() as i32, i) as *mut *mut c_void },
                None => unsafe {
                    (arr as *mut u8)
                        .add(4 * std::mem::size_of::<usize>() + i * std::mem::size_of::<*mut c_void>())
                        as *mut *mut c_void
                },
            };
            unsafe { *slot = s };
        }
        arr
    }

    fn array_elements(&self, arr: *mut c_void) -> Vec<*mut c_void> {
        if arr.is_null() {
            return Vec::new();
        }
        let raw_len = match self.array_length {
            Some(f) => unsafe { f(arr) as usize },
            None => unsafe {
                *((arr as *const u8).add(3 * std::mem::size_of::<usize>()) as *const u32) as usize
            },
        };
        let len = raw_len.min(4096);
        let mut out = Vec::with_capacity(len);
        for i in 0..len {
            let ptr = match self.array_addr_with_size {
                Some(f) => unsafe { *(f(arr, std::mem::size_of::<*mut c_void>() as i32, i) as *const *mut c_void) },
                None => unsafe {
                    *((arr as *const u8)
                        .add(4 * std::mem::size_of::<usize>() + i * std::mem::size_of::<*mut c_void>())
                        as *const *mut c_void)
                },
            };
            out.push(ptr);
        }
        out
    }

    fn gchandle_pin(&self, obj: *mut c_void) -> u32 {
        if let Some(f) = self.gchandle_new {
            if !obj.is_null() {
                return unsafe { f(obj, 1) };
            }
        }
        0
    }

    fn gchandle_free(&self, handle: u32) {
        if handle != 0 {
            if let Some(f) = self.gchandle_free {
                unsafe { f(handle) };
            }
        }
    }

    fn object_new(&self, klass: *mut c_void) -> *mut c_void {
        let dom = self.root_domain();
        match self.object_new {
            Some(f) if !dom.is_null() && !klass.is_null() => unsafe { f(dom, klass) },
            _ => std::ptr::null_mut(),
        }
    }

    fn field_from_name(&self, klass: *mut c_void, name: &str) -> *mut c_void {
        let Ok(c_name) = CString::new(name) else { return std::ptr::null_mut() };
        match self.class_get_field_from_name {
            Some(f) if !klass.is_null() => unsafe { f(klass, c_name.as_ptr()) },
            _ => std::ptr::null_mut(),
        }
    }

    fn field_get_value(&self, obj: *mut c_void, field: *mut c_void, out: *mut c_void) {
        if let Some(f) = self.field_get_value {
            if !obj.is_null() && !field.is_null() {
                unsafe { f(obj, field, out) };
            }
        }
    }

    fn field_set_value(&self, obj: *mut c_void, field: *mut c_void, val: *mut c_void) {
        if let Some(f) = self.field_set_value {
            if !obj.is_null() && !field.is_null() {
                unsafe { f(obj, field, val) };
            }
        }
    }

    fn field_static_set_value(&self, field: *mut c_void, val: *mut c_void) {
        let (Some(parent_fn), Some(vtable_fn), Some(set_fn)) =
            (self.field_get_parent, self.class_vtable, self.field_static_set_value)
        else {
            return;
        };
        let dom = self.root_domain();
        if dom.is_null() || field.is_null() {
            return;
        }
        let parent = unsafe { parent_fn(field) };
        if parent.is_null() {
            return;
        }
        let vt = unsafe { vtable_fn(dom, parent) };
        if !vt.is_null() {
            unsafe { set_fn(vt, field, val) };
        }
    }
}

struct ThreadAttachGuard<'a> {
    api: &'a MonoApi,
    thread: *mut c_void,
}

impl Drop for ThreadAttachGuard<'_> {
    fn drop(&mut self) {
        if !self.thread.is_null() {
            if let Some(detach) = self.api.thread_detach {
                unsafe { detach(self.thread) };
            }
        }
    }
}

pub fn is_ready() -> bool {
    let Some(api) = MonoApi::get() else { return false };
    let thread = api.attach_thread();
    if thread.is_null() {
        return false;
    }
    let _guard = ThreadAttachGuard { api, thread };
    let has_app = !api.find_class("", "UnityEngine", "Application").is_null();
    let has_ui = !api.find_class("", "TMPro", "TMP_Text").is_null()
        || !api.find_class("", "UnityEngine.UI", "Text").is_null()
        || !api.find_class("", "UnityEngine", "TextMesh").is_null();
    has_app && has_ui
}

pub fn install() -> usize {
    let Some(api) = MonoApi::get() else {
        crate::log(level::ERROR, "Mono C-API exports not found");
        return 0;
    };
    let thread = api.attach_thread();
    if thread.is_null() {
        crate::log(level::ERROR, "mono_thread_attach failed");
        return 0;
    }
    let _guard = ThreadAttachGuard { api, thread };

    let mut installed = 0usize;
    let mut seen = HashSet::new();

    installed += add_simple_mono(api, HOOK_TMP_SET_TEXT, "UNITY:TMP_Text.set_text", "TMP_Text::set_text", "Unity.TextMeshPro", "TMPro", "TMP_Text", "set_text", detour_tmp_set_text as *mut c_void, &ORIG_TMP_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_TEXTMESHPRO_SET_TEXT, "UNITY:TextMeshPro.set_text", "TextMeshPro::set_text", "Unity.TextMeshPro", "TMPro", "TextMeshPro", "set_text", detour_textmeshpro_set_text as *mut c_void, &ORIG_TEXTMESHPRO_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_TEXTMESHPRO_UGUI_SET_TEXT, "UNITY:TextMeshProUGUI.set_text", "TextMeshProUGUI::set_text", "Unity.TextMeshPro", "TMPro", "TextMeshProUGUI", "set_text", detour_textmeshpro_ugui_set_text as *mut c_void, &ORIG_TEXTMESHPRO_UGUI_SET_TEXT, &mut seen);

    installed += hook_settext_mono(api, HOOK_TMP_SETTEXT, "UNITY:TMP_Text.SetText", "TMP_Text::SetText", "TMP_Text", detour_tmp_settext_2 as *mut c_void, &ORIG_TMP_SETTEXT_2, detour_tmp_settext_1 as *mut c_void, &ORIG_TMP_SETTEXT_1, &mut seen);
    installed += hook_settext_mono(api, HOOK_TEXTMESHPRO_UGUI_SETTEXT, "UNITY:TextMeshProUGUI.SetText", "TextMeshProUGUI::SetText", "TextMeshProUGUI", detour_ugui_settext_2 as *mut c_void, &ORIG_TEXTMESHPRO_UGUI_SETTEXT_2, detour_ugui_settext_1 as *mut c_void, &ORIG_TEXTMESHPRO_UGUI_SETTEXT_1, &mut seen);

    installed += add_simple_mono(api, HOOK_UI_TEXT_SET_TEXT, "UNITY:UI.Text.set_text", "UI.Text::set_text", "UnityEngine.UI", "UnityEngine.UI", "Text", "set_text", detour_ui_text_set_text as *mut c_void, &ORIG_UI_TEXT_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_TEXTMESH_SET_TEXT, "UNITY:TextMesh.set_text", "TextMesh::set_text", "UnityEngine.TextRenderingModule", "UnityEngine", "TextMesh", "set_text", detour_textmesh_set_text as *mut c_void, &ORIG_TEXTMESH_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_UIE_TEXTELEMENT_SET_TEXT, "UNITY:TextElement.set_text", "TextElement::set_text", "UnityEngine.UIElementsModule", "UnityEngine.UIElements", "TextElement", "set_text", detour_uie_textelement_set_text as *mut c_void, &ORIG_UIE_TEXTELEMENT_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_UIE_TEXTFIELD_SET_VALUE, "UNITY:TextField.set_value", "TextField::set_value", "UnityEngine.UIElementsModule", "UnityEngine.UIElements", "TextField", "set_value", detour_uie_textfield_set_value as *mut c_void, &ORIG_UIE_TEXTFIELD_SET_VALUE, &mut seen);
    installed += add_simple_mono(api, HOOK_GUITEXT_SET_TEXT, "UNITY:GUIText.set_text", "GUIText::set_text", "UnityEngine.TextRenderingModule", "UnityEngine", "GUIText", "set_text", detour_guitext_set_text as *mut c_void, &ORIG_GUITEXT_SET_TEXT, &mut seen);
    installed += add_simple_mono(api, HOOK_NGUILABEL_SET_TEXT, "UNITY:UILabel.set_text", "UILabel::set_text", "UGUI", "", "UILabel", "set_text", detour_nguilabel_set_text as *mut c_void, &ORIG_NGUILABEL_SET_TEXT, &mut seen);

    let k_canvas = api.find_class("UnityEngine.UIModule", "UnityEngine", "Canvas");
    let m_canvas = api.find_method(k_canvas, "SendWillRenderCanvases", 0);
    if hook_mono(api, m_canvas, detour_canvas_will_render as *mut c_void, &ORIG_CANVAS_WILL_RENDER, &mut seen) {
        embed::set_frame_tick_active(true);
        crate::telemetry("HOOK_NATIVE", "mono::install", 0, "info", "async frame tick via Canvas::SendWillRenderCanvases");
    } else {
        let k_cam = api.find_class("UnityEngine.CoreModule", "UnityEngine", "Camera");
        let m_cam = api.find_method(k_cam, "FireOnPreCull", 1);
        if hook_mono(api, m_cam, detour_camera_pre_cull as *mut c_void, &ORIG_CAMERA_PRE_CULL, &mut seen) {
            embed::set_frame_tick_active(true);
            crate::telemetry("HOOK_NATIVE", "mono::install", 0, "info", "async frame tick via Camera::FireOnPreCull");
        }
    }

    installed
}

#[allow(clippy::too_many_arguments)]
fn add_simple_mono(
    api: &MonoApi,
    hook_id: u64,
    code: &str,
    name: &str,
    asm: &str,
    ns: &str,
    cls: &str,
    method: &str,
    detour: *mut c_void,
    slot: &AtomicPtr<c_void>,
    seen: &mut HashSet<usize>,
) -> usize {
    let k = api.find_class(asm, ns, cls);
    if k.is_null() {
        return 0;
    }
    let m = api.find_method(k, method, 1);
    if hook_mono(api, m, detour, slot, seen) {
        emit_hook(hook_id, code, name);
        1
    } else {
        0
    }
}

#[allow(clippy::too_many_arguments)]
fn hook_settext_mono(
    api: &MonoApi,
    hook_id: u64,
    code: &str,
    name: &str,
    cls_name: &str,
    detour2: *mut c_void,
    slot2: &AtomicPtr<c_void>,
    detour1: *mut c_void,
    slot1: &AtomicPtr<c_void>,
    seen: &mut HashSet<usize>,
) -> usize {
    let k = api.find_class("Unity.TextMeshPro", "TMPro", cls_name);
    if k.is_null() {
        return 0;
    }
    let mut hooked = false;
    let mut m2 = api.find_method_typed(k, "SetText", &["String", "Boolean"]);
    if m2.is_null() && api.class_get_methods.is_none() {
        m2 = api.find_method(k, "SetText", 2);
    }
    if hook_mono(api, m2, detour2, slot2, seen) {
        hooked = true;
    }
    let m1 = api.find_method_typed(k, "SetText", &["String"]);
    if hook_mono(api, m1, detour1, slot1, seen) {
        hooked = true;
    }
    if hooked {
        emit_hook(hook_id, code, name);
        1
    } else {
        0
    }
}

fn hook_mono(
    api: &MonoApi,
    method: *mut c_void,
    detour: *mut c_void,
    orig_slot: &AtomicPtr<c_void>,
    seen: &mut HashSet<usize>,
) -> bool {
    if method.is_null() {
        return false;
    }
    let target = unsafe { (api.compile_method)(method) };
    if target.is_null() || !seen.insert(target as usize) {
        return false;
    }
    match unsafe { hooks::create(target, detour) } {
        Ok(trampoline) => {
            orig_slot.store(trampoline, Ordering::Release);
            true
        }
        Err(e) => {
            crate::log(level::WARN, format!("MinHook create failed: {e}"));
            false
        }
    }
}

fn emit_hook(hook_id: u64, code: &str, name: &str) {
    super::record_and_emit_hook(hook_id, code, name);
}

fn handle_mono_text(api: &MonoApi, this: *mut c_void, text: *mut c_void, hook_id: u64) -> *mut c_void {
    let Some(units) = api.string_utf16_slice(text) else {
        return text;
    };
    if !embed::clear_text_enabled() && !super::utf16_has_translatable_letters(units) {
        return text;
    }
    let raw = String::from_utf16_lossy(units);
    let cleaned: Cow<'_, str> = if is_tmp_hook(hook_id) {
        clean_tmp_tags(&raw)
    } else {
        Cow::Borrowed(&raw)
    };
    match embed::process_unity(hook_id, caller_rel_ctx, 0, this as usize, &cleaned, &raw, || api.gchandle_pin(this)) {
        embed::ProcessOutcome::Replace { text: replaced, apply_font } => {
            if apply_font {
                font::on_text(api, this, hook_id, &replaced);
            }
            let new_str = api.string_new(&replaced);
            if new_str.is_null() { text } else { new_str }
        }
        embed::ProcessOutcome::KeepOriginal => text,
    }
}

unsafe extern "C" fn detour_canvas_will_render() {
    let orig: unsafe extern "C" fn() = load_fn(&ORIG_CANVAS_WILL_RENDER);
    if embed::has_async_work() || embed::has_async_in_flight() {
        if let Some(_guard) = hooks::Enter::try_new() {
            let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                if let Some(api) = MonoApi::get() {
                    super::on_frame_tick(api);
                }
            }));
        }
    }
    orig();
}

unsafe extern "C" fn detour_camera_pre_cull(cam: *mut c_void) {
    let orig: unsafe extern "C" fn(*mut c_void) = load_fn(&ORIG_CAMERA_PRE_CULL);
    if embed::has_async_work() || embed::has_async_in_flight() {
        if let Some(_guard) = hooks::Enter::try_new() {
            let _ = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                if let Some(api) = MonoApi::get() {
                    super::on_frame_tick(api);
                }
            }));
        }
    }
    orig(cam);
}

unsafe fn load_fn<F: Copy>(slot: &AtomicPtr<c_void>) -> F {
    let p = slot.load(Ordering::Acquire) as usize;
    std::mem::transmute_copy::<usize, F>(&p)
}

macro_rules! mono_detour_1 {
    ($fn_name:ident, $slot:ident, $hook_id:expr) => {
        unsafe extern "C" fn $fn_name(this: *mut c_void, text: *mut c_void) {
            let orig: unsafe extern "C" fn(*mut c_void, *mut c_void) = load_fn(&$slot);
            let Some(_guard) = hooks::Enter::try_new() else {
                return orig(this, text);
            };
            let next = match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                MonoApi::get().map(|api| handle_mono_text(api, this, text, $hook_id))
            })) {
                Ok(Some(n)) => n,
                Ok(None) => text,
                Err(_) => {
                    crate::telemetry(
                        "HOOK_NATIVE",
                        "mono::detour",
                        0,
                        "error",
                        &format!("panic in mono detour | hook_id={}", $hook_id),
                    );
                    text
                }
            };
            orig(this, next);
        }
    };
}

macro_rules! mono_detour_2 {
    ($fn_name:ident, $slot:ident, $hook_id:expr) => {
        unsafe extern "C" fn $fn_name(this: *mut c_void, text: *mut c_void, sync: u8) {
            let orig: unsafe extern "C" fn(*mut c_void, *mut c_void, u8) = load_fn(&$slot);
            let Some(_guard) = hooks::Enter::try_new() else {
                return orig(this, text, sync);
            };
            let next = match std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
                MonoApi::get().map(|api| handle_mono_text(api, this, text, $hook_id))
            })) {
                Ok(Some(n)) => n,
                Ok(None) => text,
                Err(_) => {
                    crate::telemetry(
                        "HOOK_NATIVE",
                        "mono::detour",
                        0,
                        "error",
                        &format!("panic in mono detour | hook_id={}", $hook_id),
                    );
                    text
                }
            };
            orig(this, next, sync);
        }
    };
}

mono_detour_1!(detour_tmp_set_text, ORIG_TMP_SET_TEXT, HOOK_TMP_SET_TEXT);
mono_detour_1!(detour_tmp_settext_1, ORIG_TMP_SETTEXT_1, HOOK_TMP_SETTEXT);
mono_detour_2!(detour_tmp_settext_2, ORIG_TMP_SETTEXT_2, HOOK_TMP_SETTEXT);
mono_detour_1!(detour_textmeshpro_set_text, ORIG_TEXTMESHPRO_SET_TEXT, HOOK_TEXTMESHPRO_SET_TEXT);
mono_detour_1!(detour_textmeshpro_ugui_set_text, ORIG_TEXTMESHPRO_UGUI_SET_TEXT, HOOK_TEXTMESHPRO_UGUI_SET_TEXT);
mono_detour_1!(detour_ugui_settext_1, ORIG_TEXTMESHPRO_UGUI_SETTEXT_1, HOOK_TEXTMESHPRO_UGUI_SETTEXT);
mono_detour_2!(detour_ugui_settext_2, ORIG_TEXTMESHPRO_UGUI_SETTEXT_2, HOOK_TEXTMESHPRO_UGUI_SETTEXT);
mono_detour_1!(detour_ui_text_set_text, ORIG_UI_TEXT_SET_TEXT, HOOK_UI_TEXT_SET_TEXT);
mono_detour_1!(detour_textmesh_set_text, ORIG_TEXTMESH_SET_TEXT, HOOK_TEXTMESH_SET_TEXT);
mono_detour_1!(detour_uie_textelement_set_text, ORIG_UIE_TEXTELEMENT_SET_TEXT, HOOK_UIE_TEXTELEMENT_SET_TEXT);
mono_detour_1!(detour_uie_textfield_set_value, ORIG_UIE_TEXTFIELD_SET_VALUE, HOOK_UIE_TEXTFIELD_SET_VALUE);
mono_detour_1!(detour_guitext_set_text, ORIG_GUITEXT_SET_TEXT, HOOK_GUITEXT_SET_TEXT);
mono_detour_1!(detour_nguilabel_set_text, ORIG_NGUILABEL_SET_TEXT, HOOK_NGUILABEL_SET_TEXT);
