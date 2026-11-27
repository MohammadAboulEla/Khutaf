//! Unity font injection (TMP asset bundles + OS dynamic fonts) and size scaling.
//! Runs exclusively on the Unity main thread from inside hooked text setters.

use std::collections::{HashMap, HashSet};
use std::ffi::c_void;
use std::path::Path;
use std::sync::{LazyLock, Mutex};

use khutaf_proto::level;
use windows_sys::Win32::Foundation::{ERROR_NO_MORE_ITEMS, ERROR_SUCCESS};
use windows_sys::Win32::System::Registry::{
    RegCloseKey, RegEnumValueW, RegOpenKeyExW, HKEY, HKEY_CURRENT_USER, HKEY_LOCAL_MACHINE,
    KEY_READ, REG_EXPAND_SZ, REG_SZ,
};

use super::{
    is_tmp_hook, is_ui_text_hook, UnityRuntime, HOOK_TEXTMESH_SET_TEXT,
};
use crate::embed;

const MAX_SCALED_CACHE: usize = 4096;
const MAX_TRIED_DONORS: usize = 16;

#[derive(Clone, Copy, Default)]
struct LayoutMethods {
    resolved: bool,
    m_get_overflow: *mut c_void,
    m_set_overflow: *mut c_void,
    m_get_max_lines: *mut c_void,
    m_set_max_lines: *mut c_void,
    m_get_max_chars: *mut c_void,
    m_set_max_chars: *mut c_void,
    m_get_v_overflow: *mut c_void,
    m_set_v_overflow: *mut c_void,
    m_get_rect_transform: *mut c_void,
    m_get_rect: *mut c_void,
    m_get_font_size: *mut c_void,
    m_get_wrap_mode: *mut c_void,
    m_set_wrap_mode: *mut c_void,
    m_get_word_wrap: *mut c_void,
    m_set_word_wrap: *mut c_void,
}

struct FontState {
    initialized: bool,
    loaded_dir: String,
    loaded_family: String,
    bundle_ab: *mut c_void,
    bundle_font: *mut c_void,
    dyn_font: *mut c_void,
    os_font: *mut c_void,
    tried_donors: HashSet<usize>,
    t_tmp: *mut c_void,
    t_fa: *mut c_void,
    f_font: *mut c_void,
    m_tmp_get_font: *mut c_void,
    m_tmp_set_font: *mut c_void,
    size_methods: HashMap<usize, (*mut c_void, *mut c_void)>,
    layout_methods: HashMap<usize, LayoutMethods>,
    bundle_handles: Vec<usize>,
    os_handles: Vec<usize>,
    scaled: HashMap<usize, u32>,
    fallback_attached: HashSet<usize>,
}

unsafe impl Send for FontState {}

static STATE: LazyLock<Mutex<FontState>> = LazyLock::new(|| {
    Mutex::new(FontState {
        initialized: false,
        loaded_dir: String::new(),
        loaded_family: String::new(),
        bundle_ab: std::ptr::null_mut(),
        bundle_font: std::ptr::null_mut(),
        dyn_font: std::ptr::null_mut(),
        os_font: std::ptr::null_mut(),
        tried_donors: HashSet::new(),
        t_tmp: std::ptr::null_mut(),
        t_fa: std::ptr::null_mut(),
        f_font: std::ptr::null_mut(),
        m_tmp_get_font: std::ptr::null_mut(),
        m_tmp_set_font: std::ptr::null_mut(),
        size_methods: HashMap::new(),
        layout_methods: HashMap::new(),
        bundle_handles: Vec::new(),
        os_handles: Vec::new(),
        scaled: HashMap::new(),
        fallback_attached: HashSet::new(),
    })
});

pub fn on_text(rt: &dyn UnityRuntime, component: *mut c_void, hook_id: u64, text: &str) {
    let settings = embed::settings();
    let dir = settings.font_dir.trim();
    let family = settings.font_family.trim();

    let mut st = STATE.lock().unwrap_or_else(|p| p.into_inner());

    if !st.initialized || st.loaded_dir != dir || st.loaded_family != family {
        reload_fonts(rt, &mut st, dir, family, component, hook_id);
    } else if st.dyn_font.is_null()
        && (!st.os_font.is_null() || !family.is_empty() || !dir.is_empty())
        && is_tmp_hook(hook_id)
        && !component.is_null()
        && st.tried_donors.len() < MAX_TRIED_DONORS
    {
        // Retry TMP font creation across distinct TMP components that can donate a shader.
        if let Some(donor) = read_component_tmp_font(rt, component) {
            if st.tried_donors.insert(donor as usize) {
                let t_retry = std::time::Instant::now();
                let font_paths = resolve_font_paths(family);
                let dyn_tmp =
                    create_tmp_font_asset(rt, st.os_font, donor, "retry", family, &font_paths);
                if !dyn_tmp.is_null() {
                    let h = rt.gchandle_pin(dyn_tmp);
                    if h != 0 {
                        st.os_handles.push(h);
                    }
                    dont_destroy(rt, dyn_tmp);
                    st.dyn_font = dyn_tmp;
                    if st.t_fa.is_null() {
                        st.t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
                    }
                    let t_fa = st.t_fa;
                    if !t_fa.is_null() {
                        if !st.bundle_font.is_null() && st.bundle_font != st.dyn_font {
                            attach_fallback_to_asset(rt, t_fa, st.dyn_font, st.bundle_font);
                            attach_fallback_to_asset(rt, t_fa, st.bundle_font, st.dyn_font);
                        }
                        register_global_fallbacks(rt, t_fa, st.bundle_font, st.dyn_font);
                    }
                    let fam_label = if family.is_empty() { "(default)" } else { family };
                    crate::telemetry(
                        "UNITY_FONT",
                        "font::on_text",
                        t_retry.elapsed().as_micros() as u64,
                        "info",
                        &format!("active=os_dynamic family={fam_label} via=shader_donor_retry"),
                    );
                }
            }
        }
    }

    if !component.is_null() {
        apply_component_font(rt, &mut st, component, hook_id);
        ensure_text_layout(rt, &mut st, component, hook_id, text);
        if settings.change_font_size
            && settings.font_scale > 0.0
            && (settings.font_scale - 1.0).abs() > 0.001
        {
            scale_component_font(rt, &mut st, component, hook_id, settings.font_scale);
        }
    }
}

fn read_component_tmp_font(rt: &dyn UnityRuntime, component: *mut c_void) -> Option<*mut c_void> {
    if component.is_null() {
        return None;
    }
    let comp_cls = rt.object_get_class(component);
    let m_get = rt.find_method_in_hierarchy(comp_cls, "get_font", 0);
    if let Some(f) = rt.invoke(m_get, component, &mut []) {
        if !f.is_null() {
            return Some(f);
        }
    }
    let f_font = rt.find_field_in_hierarchy(comp_cls, "m_fontAsset");
    if !f_font.is_null() {
        let mut cur: *mut c_void = std::ptr::null_mut();
        rt.field_get_value(component, f_font, &mut cur as *mut *mut c_void as *mut c_void);
        if !cur.is_null() {
            return Some(cur);
        }
    }
    None
}

fn reload_fonts(
    rt: &dyn UnityRuntime,
    st: &mut FontState,
    dir: &str,
    family: &str,
    component: *mut c_void,
    hook_id: u64,
) {
    let start = std::time::Instant::now();
    let dir_changed = !st.initialized || st.loaded_dir != dir;
    let fam_changed = !st.initialized || st.loaded_family != family;
    st.initialized = true;
    st.loaded_dir = dir.to_owned();
    st.loaded_family = family.to_owned();
    st.tried_donors.clear();
    st.scaled.clear();
    st.size_methods.clear();
    st.fallback_attached.clear();

    if st.t_tmp.is_null() {
        st.t_tmp = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Text");
    }
    if !st.t_tmp.is_null() {
        if st.f_font.is_null() {
            st.f_font = rt.find_field_in_hierarchy(st.t_tmp, "m_fontAsset");
        }
        if st.m_tmp_get_font.is_null() {
            st.m_tmp_get_font = rt.find_method_in_hierarchy(st.t_tmp, "get_font", 0);
        }
        if st.m_tmp_set_font.is_null() {
            st.m_tmp_set_font = rt.find_method_in_hierarchy(st.t_tmp, "set_font", 1);
        }
    }
    if st.t_fa.is_null() {
        st.t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    }

    if dir.is_empty() && family.is_empty() {
        unregister_fallback_everywhere(rt, st, st.bundle_font);
        unregister_fallback_everywhere(rt, st, st.dyn_font);
        unload_bundle(rt, st.bundle_ab);
        st.bundle_ab = std::ptr::null_mut();
        for h in st.bundle_handles.drain(..) {
            rt.gchandle_free(h);
        }
        for h in st.os_handles.drain(..) {
            rt.gchandle_free(h);
        }
        st.bundle_font = std::ptr::null_mut();
        st.dyn_font = std::ptr::null_mut();
        st.os_font = std::ptr::null_mut();
        return;
    }

    if dir_changed {
        unregister_fallback_everywhere(rt, st, st.bundle_font);
        unload_bundle(rt, st.bundle_ab);
        st.bundle_ab = std::ptr::null_mut();
        for h in st.bundle_handles.drain(..) {
            rt.gchandle_free(h);
        }
        let (ab, font) = if dir.is_empty() {
            (std::ptr::null_mut(), std::ptr::null_mut())
        } else {
            load_bundle_font(rt, dir, &mut st.bundle_handles)
        };
        st.bundle_ab = ab;
        st.bundle_font = font;
    }

    if fam_changed || (dir_changed && !st.bundle_font.is_null() && st.dyn_font.is_null()) {
        unregister_fallback_everywhere(rt, st, st.dyn_font);
        for h in st.os_handles.drain(..) {
            rt.gchandle_free(h);
        }
        let donor = if !st.bundle_font.is_null() {
            st.bundle_font
        } else if is_tmp_hook(hook_id) {
            read_component_tmp_font(rt, component).unwrap_or(std::ptr::null_mut())
        } else {
            std::ptr::null_mut()
        };
        let (os, dyn_tmp) = load_os_fonts(rt, family, donor, &mut st.os_handles);
        st.os_font = os;
        st.dyn_font = dyn_tmp;
    }

    let t_fa = st.t_fa;
    if !t_fa.is_null() {
        if !st.dyn_font.is_null() && !st.bundle_font.is_null() && st.dyn_font != st.bundle_font {
            attach_fallback_to_asset(rt, t_fa, st.dyn_font, st.bundle_font);
            attach_fallback_to_asset(rt, t_fa, st.bundle_font, st.dyn_font);
        }
        register_global_fallbacks(rt, t_fa, st.bundle_font, st.dyn_font);
    }

    let (active, reason) = if !st.dyn_font.is_null() {
        ("os_dynamic", "ok")
    } else if !st.bundle_font.is_null() {
        ("bundle_fallback", "os_tmp_failed")
    } else if !st.os_font.is_null() {
        ("os_ui_only", "tmp_unavailable")
    } else {
        ("none", "no_font_loaded")
    };
    let fam_label = if family.is_empty() { "(default)" } else { family };
    crate::telemetry(
        "UNITY_FONT",
        "font::reload_fonts",
        start.elapsed().as_micros() as u64,
        "info",
        &format!(
            "active={active} reason={reason} family={fam_label} bundle_ready={} os_tmp={} os_ui={}",
            !st.bundle_font.is_null(),
            !st.dyn_font.is_null(),
            !st.os_font.is_null()
        ),
    );
}

fn pick_bundle(unity_version: &str) -> &'static str {
    let major: u32 = unity_version
        .split('.')
        .next()
        .and_then(|s| s.parse().ok())
        .unwrap_or(0);
    if major >= 6000 {
        "arialuni_sdf_u6000"
    } else if major >= 2022 {
        "arialuni_sdf_u2022"
    } else if major >= 2020 {
        "arialuni_sdf_u2021"
    } else if major == 2019 {
        "arialuni_sdf_u2019"
    } else if major == 2018 {
        "arialuni_sdf_u2018"
    } else {
        "arialuni_sdf-u55to2017"
    }
}

fn unity_version(rt: &dyn UnityRuntime) -> String {
    let app = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Application");
    if !app.is_null() {
        let m = rt.find_method(app, "get_unityVersion", 0);
        if let Some(res) = rt.invoke(m, std::ptr::null_mut(), &mut []) {
            let v = rt.string_to_rust(res);
            if !v.is_empty() {
                return v;
            }
        }
    }
    let icall = rt.resolve_icall("UnityEngine.Application::get_unityVersion");
    if !icall.is_null() {
        let f: unsafe extern "C" fn() -> *mut c_void = unsafe { std::mem::transmute_copy(&icall) };
        let res = unsafe { f() };
        if !res.is_null() {
            return rt.string_to_rust(res);
        }
    }
    String::new()
}

fn dont_destroy(rt: &dyn UnityRuntime, obj: *mut c_void) {
    if obj.is_null() {
        return;
    }
    let t_obj = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Object");
    if !t_obj.is_null() {
        let m = rt.find_method(t_obj, "DontDestroyOnLoad", 1);
        if !m.is_null() {
            let mut args = [obj];
            let _ = rt.invoke(m, std::ptr::null_mut(), &mut args);
            return;
        }
    }
    let icall = rt.resolve_icall("UnityEngine.Object::DontDestroyOnLoad");
    if !icall.is_null() {
        let f: unsafe extern "C" fn(*mut c_void) = unsafe { std::mem::transmute_copy(&icall) };
        unsafe { f(obj) };
    }
}

fn unload_bundle(rt: &dyn UnityRuntime, ab: *mut c_void) {
    if ab.is_null() {
        return;
    }
    let t_ab = rt.find_class("UnityEngine.AssetBundleModule", "UnityEngine", "AssetBundle");
    if !t_ab.is_null() {
        let m_unload = rt.find_method(t_ab, "Unload", 1);
        if !m_unload.is_null() {
            let mut unload_all: u8 = 0;
            let _ = rt.invoke(m_unload, ab, &mut [&mut unload_all as *mut u8 as *mut c_void]);
            return;
        }
    }
    let icall = rt.resolve_icall("UnityEngine.AssetBundle::Unload");
    if !icall.is_null() {
        let f: unsafe extern "C" fn(*mut c_void, u8) = unsafe { std::mem::transmute_copy(&icall) };
        unsafe { f(ab, 0) };
    }
}

fn load_bundle_font(
    rt: &dyn UnityRuntime,
    dir: &str,
    handles: &mut Vec<usize>,
) -> (*mut c_void, *mut c_void) {
    let ver = unity_version(rt);
    let path = Path::new(dir).join(pick_bundle(&ver));
    if !path.exists() {
        crate::telemetry(
            "UNITY_FONT",
            "font::load_bundle_font",
            0,
            "warn",
            &format!("TMP bundle missing | unity={ver}"),
        );
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }
    let Some(path_str_arg) = path.to_str() else {
        return (std::ptr::null_mut(), std::ptr::null_mut());
    };
    let path_managed = rt.string_new(path_str_arg);
    if path_managed.is_null() {
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }

    let t_ab = rt.find_class("UnityEngine.AssetBundleModule", "UnityEngine", "AssetBundle");
    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if t_fa.is_null() {
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }

    let mut ab = std::ptr::null_mut();
    if !t_ab.is_null() {
        let m_load = rt.find_method_param0(t_ab, "LoadFromFile", 1, "String");
        if let Some(res) = rt.invoke(m_load, std::ptr::null_mut(), &mut [path_managed]) {
            ab = res;
        }
        if ab.is_null() {
            let m_load3 = rt.find_method_param0(t_ab, "LoadFromFile", 3, "String");
            if !m_load3.is_null() {
                let mut crc: u32 = 0;
                let mut offset: u64 = 0;
                let mut args = [
                    path_managed,
                    &mut crc as *mut u32 as *mut c_void,
                    &mut offset as *mut u64 as *mut c_void,
                ];
                if let Some(res) = rt.invoke(m_load3, std::ptr::null_mut(), &mut args) {
                    ab = res;
                }
            }
        }
    }
    if ab.is_null() {
        let icall = rt.resolve_icall("UnityEngine.AssetBundle::LoadFromFile_Internal");
        if !icall.is_null() {
            let f: unsafe extern "C" fn(*mut c_void, u32, u64) -> *mut c_void =
                unsafe { std::mem::transmute_copy(&icall) };
            ab = unsafe { f(path_managed, 0, 0) };
        }
    }
    if ab.is_null() {
        crate::telemetry(
            "UNITY_FONT",
            "font::load_bundle_font",
            0,
            "warn",
            &format!("AssetBundle load failed | unity={ver}"),
        );
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }
    let h_ab = rt.gchandle_pin(ab);
    if h_ab != 0 {
        handles.push(h_ab);
    }
    dont_destroy(rt, ab);

    let mut arr = std::ptr::null_mut();
    if !t_ab.is_null() {
        let m_all = rt.find_method(t_ab, "LoadAllAssets", 0);
        if let Some(res) = rt.invoke(m_all, ab, &mut []) {
            arr = res;
        }
        if arr.is_null() {
            let m_all_ty = rt.find_method_param0(t_ab, "LoadAllAssets", 1, "Type");
            let type_obj = rt.class_get_type_object(t_fa);
            if !m_all_ty.is_null() && !type_obj.is_null() {
                if let Some(res) = rt.invoke(m_all_ty, ab, &mut [type_obj]) {
                    arr = res;
                }
            }
        }
    }
    if arr.is_null() {
        let icall = rt.resolve_icall("UnityEngine.AssetBundle::LoadAssetWithSubAssets_Internal");
        let type_obj = rt.class_get_type_object(t_fa);
        if !icall.is_null() && !type_obj.is_null() {
            let f: unsafe extern "C" fn(*mut c_void, *mut c_void, *mut c_void) -> *mut c_void =
                unsafe { std::mem::transmute_copy(&icall) };
            arr = unsafe { f(ab, rt.string_new(""), type_obj) };
        }
    }

    for elem in rt.array_elements(arr) {
        if elem.is_null() {
            continue;
        }
        let k = rt.object_get_class(elem);
        if rt.class_is_assignable_from(t_fa, k) {
            let h = rt.gchandle_pin(elem);
            if h != 0 {
                handles.push(h);
            }
            dont_destroy(rt, elem);
            return (ab, elem);
        }
    }
    crate::telemetry(
        "UNITY_FONT",
        "font::load_bundle_font",
        0,
        "warn",
        &format!("TMP_FontAsset missing in bundle | unity={ver}"),
    );
    unload_bundle(rt, ab);
    (std::ptr::null_mut(), std::ptr::null_mut())
}

fn match_font_reg_name(reg_name: &str, family_lc: &str) -> Option<u8> {
    let clean = reg_name
        .split('(')
        .next()
        .unwrap_or(reg_name)
        .trim()
        .to_ascii_lowercase();
    if clean == family_lc {
        return Some(0);
    }
    if clean.starts_with(family_lc) && clean.as_bytes().get(family_lc.len()) == Some(&b' ') {
        let rest = &clean[family_lc.len()..];
        let is_regular = rest.contains("regular");
        let is_styled = rest.contains("bold")
            || rest.contains("italic")
            || rest.contains("light")
            || rest.contains("thin")
            || rest.contains("black");
        return Some(if is_regular && !is_styled {
            1
        } else if !is_styled {
            2
        } else {
            3
        });
    }
    None
}

fn resolve_font_paths(family: &str) -> Vec<String> {
    let fam_lc = family.trim().to_ascii_lowercase();
    let mut ranked: Vec<(u8, String)> = Vec::new();
    if !fam_lc.is_empty() {
        let subkey: Vec<u16> = "SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\Fonts\0"
            .encode_utf16()
            .collect();
        for hive in [HKEY_LOCAL_MACHINE, HKEY_CURRENT_USER] {
            let mut hkey: HKEY = std::ptr::null_mut();
            if unsafe { RegOpenKeyExW(hive, subkey.as_ptr(), 0, KEY_READ, &mut hkey) } != ERROR_SUCCESS {
                continue;
            }
            for index in 0u32..4096 {
                let mut name_buf = [0u16; 256];
                let mut data_buf = [0u16; 512];
                let mut name_len: u32 = name_buf.len() as u32;
                let mut data_bytes: u32 = (data_buf.len() * 2) as u32;
                let mut val_type: u32 = 0;
                let res = unsafe {
                    RegEnumValueW(
                        hkey,
                        index,
                        name_buf.as_mut_ptr(),
                        &mut name_len,
                        std::ptr::null_mut(),
                        &mut val_type,
                        data_buf.as_mut_ptr() as *mut u8,
                        &mut data_bytes,
                    )
                };
                if res == ERROR_NO_MORE_ITEMS {
                    break;
                }
                if res != ERROR_SUCCESS || (val_type != REG_SZ && val_type != REG_EXPAND_SZ) {
                    continue;
                }
                let reg_name = String::from_utf16_lossy(&name_buf[..name_len as usize]);
                let Some(rank) = match_font_reg_name(&reg_name, &fam_lc) else {
                    continue;
                };
                let wlen = (data_bytes as usize) / 2;
                let file_raw = String::from_utf16_lossy(&data_buf[..wlen])
                    .trim_end_matches('\0')
                    .trim()
                    .to_owned();
                if file_raw.is_empty() {
                    continue;
                }
                let full_path = if file_raw.contains(':') || file_raw.starts_with('\\') {
                    file_raw
                } else {
                    format!(r"C:\Windows\Fonts\{file_raw}")
                };
                let ext = Path::new(&full_path)
                    .extension()
                    .and_then(|e| e.to_str())
                    .map(|e| e.to_ascii_lowercase())
                    .unwrap_or_default();
                if matches!(ext.as_str(), "ttf" | "otf" | "ttc") && Path::new(&full_path).exists() {
                    ranked.push((rank, full_path));
                }
            }
            unsafe { RegCloseKey(hkey) };
        }
        ranked.sort_by(|a, b| a.0.cmp(&b.0).then_with(|| a.1.cmp(&b.1)));
    }

    let mut out = Vec::new();
    for (_, p) in ranked {
        if !out.iter().any(|x: &String| x.eq_ignore_ascii_case(&p)) {
            out.push(p);
        }
    }
    for fb in [
        r"C:\Windows\Fonts\tahoma.ttf",
        r"C:\Windows\Fonts\arial.ttf",
        r"C:\Windows\Fonts\segoeui.ttf",
    ] {
        if Path::new(fb).exists() && !out.iter().any(|x| x.eq_ignore_ascii_case(fb)) {
            out.push(fb.to_owned());
        }
    }
    out
}

fn is_unity_obj_valid(rt: &dyn UnityRuntime, obj: *mut c_void) -> bool {
    if obj.is_null() {
        return false;
    }
    let t_obj = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Object");
    if !t_obj.is_null() {
        let f_ptr = rt.find_field_in_hierarchy(t_obj, "m_CachedPtr");
        if !f_ptr.is_null() {
            let mut ptr: *mut c_void = std::ptr::null_mut();
            rt.field_get_value(obj, f_ptr, &mut ptr as *mut *mut c_void as *mut c_void);
            return !ptr.is_null();
        }
    }
    true
}

fn create_font_from_ttf_path(
    rt: &dyn UnityRuntime,
    t_font: *mut c_void,
    ttf_path: &str,
) -> *mut c_void {
    let icall = rt.resolve_icall("UnityEngine.Font::Internal_CreateFontFromPath");
    if icall.is_null() {
        return std::ptr::null_mut();
    }
    let s = rt.string_new(ttf_path);
    if s.is_null() {
        return std::ptr::null_mut();
    }
    let obj = rt.object_new(t_font);
    if obj.is_null() {
        return std::ptr::null_mut();
    }
    let f: unsafe extern "C" fn(*mut c_void, *mut c_void) =
        unsafe { std::mem::transmute_copy(&icall) };
    unsafe { f(obj, s) };
    if is_unity_obj_valid(rt, obj) {
        obj
    } else {
        std::ptr::null_mut()
    }
}

fn load_os_fonts(
    rt: &dyn UnityRuntime,
    family: &str,
    donor_font: *mut c_void,
    handles: &mut Vec<usize>,
) -> (*mut c_void, *mut c_void) {
    let font_paths = resolve_font_paths(family);
    let t_font = rt.find_class("UnityEngine.TextRenderingModule", "UnityEngine", "Font");

    let mut os_font = std::ptr::null_mut();
    let mut os_src = "none";

    if !t_font.is_null() {
        let names: Vec<&str> = if family.is_empty() {
            vec!["Tahoma", "Arial", "Segoe UI"]
        } else {
            vec![family, "Tahoma", "Arial", "Segoe UI"]
        };
        let names_arr = rt.string_array_new(&names);

        if !names_arr.is_null() {
            let m_dyn_arr = rt.find_method_param0(t_font, "CreateDynamicFontFromOSFont", 2, "String[]");
            if !m_dyn_arr.is_null() {
                let mut size: i32 = 32;
                let mut args = [names_arr, &mut size as *mut i32 as *mut c_void];
                if let Some(res) = rt.invoke(m_dyn_arr, std::ptr::null_mut(), &mut args) {
                    if is_unity_obj_valid(rt, res) {
                        os_font = res;
                        os_src = "managed_arr";
                    }
                }
            }
        }

        if os_font.is_null() {
            let m_dyn_str = rt.find_method_param0(t_font, "CreateDynamicFontFromOSFont", 2, "String");
            if !m_dyn_str.is_null() {
                let s = rt.string_new(names[0]);
                if !s.is_null() {
                    let mut size: i32 = 32;
                    let mut args = [s, &mut size as *mut i32 as *mut c_void];
                    if let Some(res) = rt.invoke(m_dyn_str, std::ptr::null_mut(), &mut args) {
                        if is_unity_obj_valid(rt, res) {
                            os_font = res;
                            os_src = "managed_str";
                        }
                    }
                }
            }
        }

        if os_font.is_null() && !names_arr.is_null() {
            let icall = rt.resolve_icall("UnityEngine.Font::Internal_CreateDynamicFont");
            if !icall.is_null() {
                let obj = rt.object_new(t_font);
                if !obj.is_null() {
                    let f: unsafe extern "C" fn(*mut c_void, *mut c_void, i32) =
                        unsafe { std::mem::transmute_copy(&icall) };
                    unsafe { f(obj, names_arr, 32) };
                    if is_unity_obj_valid(rt, obj) {
                        os_font = obj;
                        os_src = "icall_dyn";
                    }
                }
            }
        }

        if os_font.is_null() {
            for ttf in &font_paths {
                let obj = create_font_from_ttf_path(rt, t_font, ttf);
                if !obj.is_null() {
                    os_font = obj;
                    os_src = "icall_path";
                    break;
                }
            }
        }
    }

    if !os_font.is_null() {
        let h_os = rt.gchandle_pin(os_font);
        if h_os != 0 {
            handles.push(h_os);
        }
        dont_destroy(rt, os_font);
    }

    let mut dyn_tmp =
        create_tmp_font_asset(rt, os_font, donor_font, os_src, family, &font_paths);
    if dyn_tmp.is_null() && !t_font.is_null() && os_src != "icall_path" {
        for ttf in &font_paths {
            let path_font = create_font_from_ttf_path(rt, t_font, ttf);
            if path_font.is_null() {
                continue;
            }
            let candidate =
                create_tmp_font_asset(rt, path_font, donor_font, "icall_path", "", &[]);
            if !candidate.is_null() {
                let h_path = rt.gchandle_pin(path_font);
                if h_path != 0 {
                    handles.push(h_path);
                }
                dont_destroy(rt, path_font);
                os_font = path_font;
                dyn_tmp = candidate;
                break;
            }
        }
    }
    if !dyn_tmp.is_null() {
        let h_dyn = rt.gchandle_pin(dyn_tmp);
        if h_dyn != 0 {
            handles.push(h_dyn);
        }
        dont_destroy(rt, dyn_tmp);
    }
    (os_font, dyn_tmp)
}

fn find_shader_by_name(rt: &dyn UnityRuntime, name: &str) -> *mut c_void {
    let name_managed = rt.string_new(name);
    if name_managed.is_null() {
        return std::ptr::null_mut();
    }
    let t_shader = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Shader");
    if !t_shader.is_null() {
        let m_find = rt.find_method_param0(t_shader, "Find", 1, "String");
        if let Some(sh) = rt.invoke(m_find, std::ptr::null_mut(), &mut [name_managed]) {
            if !sh.is_null() {
                return sh;
            }
        }
    }
    let icall = rt.resolve_icall("UnityEngine.Shader::Find");
    if !icall.is_null() {
        let f: unsafe extern "C" fn(*mut c_void) -> *mut c_void =
            unsafe { std::mem::transmute_copy(&icall) };
        let sh = unsafe { f(name_managed) };
        if !sh.is_null() {
            return sh;
        }
    }
    std::ptr::null_mut()
}

fn extract_shader_from_font_asset(
    rt: &dyn UnityRuntime,
    t_fa: *mut c_void,
    asset: *mut c_void,
) -> *mut c_void {
    if asset.is_null() {
        return std::ptr::null_mut();
    }
    let mut mat_obj: *mut c_void = std::ptr::null_mut();
    let asset_cls = rt.object_get_class(asset);
    let m_get_mat = rt.find_method_in_hierarchy(asset_cls, "get_material", 0);
    if let Some(m) = rt.invoke(m_get_mat, asset, &mut []) {
        mat_obj = m;
    }
    if mat_obj.is_null() {
        let mut f_mat = rt.find_field_in_hierarchy(t_fa, "material");
        if f_mat.is_null() {
            f_mat = rt.find_field_in_hierarchy(t_fa, "m_Material");
        }
        if !f_mat.is_null() {
            rt.field_get_value(asset, f_mat, &mut mat_obj as *mut *mut c_void as *mut c_void);
        }
    }
    if mat_obj.is_null() {
        return std::ptr::null_mut();
    }
    let icall_sh = rt.resolve_icall("UnityEngine.Material::get_shader");
    if !icall_sh.is_null() {
        let f: unsafe extern "C" fn(*mut c_void) -> *mut c_void =
            unsafe { std::mem::transmute_copy(&icall_sh) };
        return unsafe { f(mat_obj) };
    }
    let t_mat = rt.object_get_class(mat_obj);
    let m_sh = rt.find_method(t_mat, "get_shader", 0);
    rt.invoke(m_sh, mat_obj, &mut []).unwrap_or(std::ptr::null_mut())
}

fn ensure_mobile_sdf_shader(rt: &dyn UnityRuntime, t_fa: *mut c_void, donor_font: *mut c_void) -> &'static str {
    let t_shader_util = rt.find_class("Unity.TextMeshPro", "TMPro", "ShaderUtilities");
    if t_shader_util.is_null() {
        return "no_shader_util";
    }
    rt.class_init(t_shader_util);

    let m_props = rt.find_method(t_shader_util, "GetShaderPropertyIDs", 0);
    if !m_props.is_null() {
        let _ = rt.invoke(m_props, std::ptr::null_mut(), &mut []);
    }

    let m_get_shader = rt.find_method(t_shader_util, "get_ShaderRef_MobileSDF", 0);
    if let Some(sh) = rt.invoke(m_get_shader, std::ptr::null_mut(), &mut []) {
        if !sh.is_null() {
            return "existing";
        }
    }

    let mut source = "missing";
    let mut shader = find_shader_by_name(rt, "TextMeshPro/Mobile/Distance Field");
    if !shader.is_null() {
        source = "shader_find_mobile";
    }
    if shader.is_null() {
        shader = find_shader_by_name(rt, "TextMeshPro/Distance Field");
        if !shader.is_null() {
            source = "shader_find_sdf";
        }
    }
    if shader.is_null() && !donor_font.is_null() {
        shader = extract_shader_from_font_asset(rt, t_fa, donor_font);
        if !shader.is_null() {
            source = "donor";
        }
    }
    if shader.is_null() {
        let t_settings = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Settings");
        let m_def = rt.find_method(t_settings, "get_defaultFontAsset", 0);
        if let Some(def_font) = rt.invoke(m_def, std::ptr::null_mut(), &mut []) {
            shader = extract_shader_from_font_asset(rt, t_fa, def_font);
            if !shader.is_null() {
                source = "default_font";
            }
        }
    }
    if shader.is_null() {
        let t_res = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "Resources");
        let type_obj = rt.class_get_type_object(t_fa);
        if !t_res.is_null() && !type_obj.is_null() {
            let m_find_all = rt.find_method_param0(t_res, "FindObjectsOfTypeAll", 1, "Type");
            if let Some(arr) = rt.invoke(m_find_all, std::ptr::null_mut(), &mut [type_obj]) {
                for elem in rt.array_elements(arr) {
                    shader = extract_shader_from_font_asset(rt, t_fa, elem);
                    if !shader.is_null() {
                        source = "resources_scan";
                        break;
                    }
                }
            }
        }
    }
    if !shader.is_null() {
        let f_ref = rt.field_from_name(t_shader_util, "k_ShaderRef_MobileSDF");
        if f_ref.is_null() {
            return "field_missing";
        }
        rt.field_static_set_value(f_ref, shader);
    }
    source
}

fn enable_multi_atlas(rt: &dyn UnityRuntime, t_fa: *mut c_void, fa: *mut c_void) {
    if fa.is_null() || t_fa.is_null() {
        return;
    }
    let m_set = rt.find_method_in_hierarchy(t_fa, "set_isMultiAtlasTexturesEnabled", 1);
    if !m_set.is_null() {
        let mut enabled: u8 = 1;
        let _ = rt.invoke(m_set, fa, &mut [&mut enabled as *mut u8 as *mut c_void]);
    }
}

fn create_tmp_font_asset(
    rt: &dyn UnityRuntime,
    os_font: *mut c_void,
    donor_font: *mut c_void,
    os_src: &str,
    family: &str,
    font_paths: &[String],
) -> *mut c_void {
    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if t_fa.is_null() {
        crate::telemetry("UNITY_FONT", "font::create_tmp_font_asset", 0, "warn", "failed | no TMP_FontAsset");
        return std::ptr::null_mut();
    }
    let shader_src = ensure_mobile_sdf_shader(rt, t_fa, donor_font);

    let m1 = rt.find_method_param0(t_fa, "CreateFontAsset", 1, "Font");
    let has_m1 = !m1.is_null();
    if has_m1 && !os_font.is_null() {
        if let Some(fa) = rt.invoke(m1, std::ptr::null_mut(), &mut [os_font]) {
            if is_unity_obj_valid(rt, fa) {
                return fa;
            }
        }
        crate::log(level::WARN, "TMP_FontAsset.CreateFontAsset(Font) returned null");
    }

    let mut face_index: i32 = 0;
    let mut sampling: i32 = 90;
    let mut padding: i32 = 9;
    let mut render_mode: i32 = 4165; // GlyphRenderMode.SDFAA
    let mut width: i32 = 1024;
    let mut height: i32 = 1024;
    let mut pop_mode: i32 = 1; // AtlasPopulationMode.Dynamic
    let mut multi_atlas: u8 = 1;

    let m9 = rt.find_method_param0(t_fa, "CreateFontAsset", 9, "Font");
    let has_m9 = !m9.is_null();
    if has_m9 && !os_font.is_null() {
        let mut args = [
            os_font,
            &mut face_index as *mut i32 as *mut c_void,
            &mut sampling as *mut i32 as *mut c_void,
            &mut padding as *mut i32 as *mut c_void,
            &mut render_mode as *mut i32 as *mut c_void,
            &mut width as *mut i32 as *mut c_void,
            &mut height as *mut i32 as *mut c_void,
            &mut pop_mode as *mut i32 as *mut c_void,
            &mut multi_atlas as *mut u8 as *mut c_void,
        ];
        if let Some(fa) = rt.invoke(m9, std::ptr::null_mut(), &mut args) {
            if is_unity_obj_valid(rt, fa) {
                return fa;
            }
        }
    }

    let m8 = rt.find_method_param0(t_fa, "CreateFontAsset", 8, "Font");
    let has_m8 = !m8.is_null();
    if has_m8 && !os_font.is_null() {
        let mut args = [
            os_font,
            &mut sampling as *mut i32 as *mut c_void,
            &mut padding as *mut i32 as *mut c_void,
            &mut render_mode as *mut i32 as *mut c_void,
            &mut width as *mut i32 as *mut c_void,
            &mut height as *mut i32 as *mut c_void,
            &mut pop_mode as *mut i32 as *mut c_void,
            &mut multi_atlas as *mut u8 as *mut c_void,
        ];
        if let Some(fa) = rt.invoke(m8, std::ptr::null_mut(), &mut args) {
            if is_unity_obj_valid(rt, fa) {
                return fa;
            }
        }
    }

    let m7 = rt.find_method_param0(t_fa, "CreateFontAsset", 7, "Font");
    let has_m7 = !m7.is_null();
    if has_m7 && !os_font.is_null() {
        let mut args = [
            os_font,
            &mut sampling as *mut i32 as *mut c_void,
            &mut padding as *mut i32 as *mut c_void,
            &mut render_mode as *mut i32 as *mut c_void,
            &mut width as *mut i32 as *mut c_void,
            &mut height as *mut i32 as *mut c_void,
            &mut pop_mode as *mut i32 as *mut c_void,
        ];
        if let Some(fa) = rt.invoke(m7, std::ptr::null_mut(), &mut args) {
            if is_unity_obj_valid(rt, fa) {
                return fa;
            }
        }
    }

    // Unity 6 / TMP 3.2+: CreateFontAsset(string fontFilePath, int faceIndex, int samplingPointSize, ...)
    let m_str7 = rt.find_method_param0(t_fa, "CreateFontAsset", 7, "String");
    let has_str7 = !m_str7.is_null();
    if has_str7 {
        for ttf in font_paths {
            let path_str = rt.string_new(ttf);
            if path_str.is_null() {
                continue;
            }
            let mut args = [
                path_str,
                &mut face_index as *mut i32 as *mut c_void,
                &mut sampling as *mut i32 as *mut c_void,
                &mut padding as *mut i32 as *mut c_void,
                &mut render_mode as *mut i32 as *mut c_void,
                &mut width as *mut i32 as *mut c_void,
                &mut height as *mut i32 as *mut c_void,
            ];
            if let Some(fa) = rt.invoke(m_str7, std::ptr::null_mut(), &mut args) {
                if is_unity_obj_valid(rt, fa) {
                    enable_multi_atlas(rt, t_fa, fa);
                    return fa;
                }
            }
        }
    }

    // Unity 6 / TMP 3.2+: CreateFontAsset(string familyName, string styleName, int pointSize)
    let m_str3 = rt.find_method_param0(t_fa, "CreateFontAsset", 3, "String");
    let has_str3 = !m_str3.is_null();
    if has_str3 {
        let style_str = rt.string_new("Regular");
        if !style_str.is_null() {
            let mut families: Vec<&str> = Vec::with_capacity(4);
            let fam_trimmed = family.trim();
            if !fam_trimmed.is_empty() {
                families.push(fam_trimmed);
            }
            for fb in ["Tahoma", "Arial", "Segoe UI"] {
                if !families.iter().any(|x| x.eq_ignore_ascii_case(fb)) {
                    families.push(fb);
                }
            }
            for fam in families {
                let fam_str = rt.string_new(fam);
                if fam_str.is_null() {
                    continue;
                }
                let mut args = [
                    fam_str,
                    style_str,
                    &mut sampling as *mut i32 as *mut c_void,
                ];
                if let Some(fa) = rt.invoke(m_str3, std::ptr::null_mut(), &mut args) {
                    if is_unity_obj_valid(rt, fa) {
                        enable_multi_atlas(rt, t_fa, fa);
                        return fa;
                    }
                }
            }
        }
    }

    crate::telemetry(
        "UNITY_FONT",
        "font::create_tmp_font_asset",
        0,
        "warn",
        &format!(
            "failed | os_src={os_src} shader={shader_src} m1={has_m1} m9={has_m9} m8={has_m8} m7={has_m7} str7={has_str7} str3={has_str3}"
        ),
    );
    std::ptr::null_mut()
}

fn remove_from_font_list(rt: &dyn UnityRuntime, list: *mut c_void, font_asset: *mut c_void) {
    if list.is_null() || font_asset.is_null() {
        return;
    }
    let t_list = rt.object_get_class(list);
    if t_list.is_null() {
        return;
    }
    let m_remove = rt.find_method_in_hierarchy(t_list, "Remove", 1);
    if !m_remove.is_null() {
        let _ = rt.invoke(m_remove, list, &mut [font_asset]);
    }
}

fn detach_fallback_from_asset(
    rt: &dyn UnityRuntime,
    t_fa: *mut c_void,
    target_font: *mut c_void,
    fallback: *mut c_void,
) {
    if target_font.is_null() || fallback.is_null() || target_font == fallback {
        return;
    }
    let mut m_get = rt.find_method_in_hierarchy(t_fa, "get_fallbackFontAssetTable", 0);
    if m_get.is_null() {
        m_get = rt.find_method_in_hierarchy(t_fa, "get_fallbackFontAssets", 0);
    }
    if let Some(fb_list) = rt.invoke(m_get, target_font, &mut []) {
        remove_from_font_list(rt, fb_list, fallback);
    }
}

fn unregister_fallback_everywhere(rt: &dyn UnityRuntime, st: &FontState, old_font: *mut c_void) {
    if old_font.is_null() || st.t_fa.is_null() {
        return;
    }
    let t_settings = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Settings");
    if !t_settings.is_null() {
        let m_fb = rt.find_method(t_settings, "get_fallbackFontAssets", 0);
        if let Some(fb_list) = rt.invoke(m_fb, std::ptr::null_mut(), &mut []) {
            remove_from_font_list(rt, fb_list, old_font);
        }
        let m_def = rt.find_method(t_settings, "get_defaultFontAsset", 0);
        if let Some(def_font) = rt.invoke(m_def, std::ptr::null_mut(), &mut []) {
            detach_fallback_from_asset(rt, st.t_fa, def_font, old_font);
        }
    }
    if !st.bundle_font.is_null() && st.bundle_font != old_font {
        detach_fallback_from_asset(rt, st.t_fa, st.bundle_font, old_font);
    }
    if !st.dyn_font.is_null() && st.dyn_font != old_font {
        detach_fallback_from_asset(rt, st.t_fa, st.dyn_font, old_font);
    }
}

fn add_to_font_list(rt: &dyn UnityRuntime, list: *mut c_void, font_asset: *mut c_void) {
    if list.is_null() || font_asset.is_null() {
        return;
    }
    let t_list = rt.object_get_class(list);
    if t_list.is_null() {
        return;
    }
    let m_contains = rt.find_method_in_hierarchy(t_list, "Contains", 1);
    if let Some(boxed) = rt.invoke(m_contains, list, &mut [font_asset]) {
        let ptr = rt.unbox(boxed) as *const u8;
        if !ptr.is_null() && unsafe { *ptr } != 0 {
            return;
        }
    }
    let m_add = rt.find_method_in_hierarchy(t_list, "Add", 1);
    let _ = rt.invoke(m_add, list, &mut [font_asset]);
}

fn attach_fallback_to_asset(
    rt: &dyn UnityRuntime,
    t_fa: *mut c_void,
    target_font: *mut c_void,
    fallback: *mut c_void,
) {
    if target_font.is_null() || fallback.is_null() || target_font == fallback {
        return;
    }
    let mut m_get = rt.find_method_in_hierarchy(t_fa, "get_fallbackFontAssetTable", 0);
    if m_get.is_null() {
        m_get = rt.find_method_in_hierarchy(t_fa, "get_fallbackFontAssets", 0);
    }
    let mut fb_list = rt.invoke(m_get, target_font, &mut []).unwrap_or(std::ptr::null_mut());
    if fb_list.is_null() {
        let t_settings = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Settings");
        let m_global_fb = rt.find_method(t_settings, "get_fallbackFontAssets", 0);
        if let Some(global_list) = rt.invoke(m_global_fb, std::ptr::null_mut(), &mut []) {
            let list_cls = rt.object_get_class(global_list);
            let new_list = rt.object_new(list_cls);
            if !new_list.is_null() {
                let ctor = rt.find_method(list_cls, ".ctor", 0);
                let _ = rt.invoke(ctor, new_list, &mut []);
                let mut m_set = rt.find_method_in_hierarchy(t_fa, "set_fallbackFontAssetTable", 1);
                if m_set.is_null() {
                    m_set = rt.find_method_in_hierarchy(t_fa, "set_fallbackFontAssets", 1);
                }
                if !m_set.is_null() {
                    let _ = rt.invoke(m_set, target_font, &mut [new_list]);
                    fb_list = new_list;
                }
            }
        }
    }
    if !fb_list.is_null() {
        add_to_font_list(rt, fb_list, fallback);
    }
}

fn register_global_fallbacks(
    rt: &dyn UnityRuntime,
    t_fa: *mut c_void,
    bundle_font: *mut c_void,
    dyn_font: *mut c_void,
) {
    let t_settings = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Settings");
    if t_settings.is_null() {
        return;
    }
    let m_fb = rt.find_method(t_settings, "get_fallbackFontAssets", 0);
    if let Some(fb_list) = rt.invoke(m_fb, std::ptr::null_mut(), &mut []) {
        if !dyn_font.is_null() {
            add_to_font_list(rt, fb_list, dyn_font);
        }
        if !bundle_font.is_null() {
            add_to_font_list(rt, fb_list, bundle_font);
        }
    }

    let m_def = rt.find_method(t_settings, "get_defaultFontAsset", 0);
    if let Some(def_font) = rt.invoke(m_def, std::ptr::null_mut(), &mut []) {
        if !def_font.is_null() {
            attach_fallback_to_asset(rt, t_fa, def_font, dyn_font);
            attach_fallback_to_asset(rt, t_fa, def_font, bundle_font);
        }
    }
}

fn apply_component_font(
    rt: &dyn UnityRuntime,
    st: &mut FontState,
    component: *mut c_void,
    hook_id: u64,
) {
    if is_ui_text_hook(hook_id) {
        if st.os_font.is_null() {
            return;
        }
        let k = rt.object_get_class(component);
        let m_get = rt.find_method_in_hierarchy(k, "get_font", 0);
        if let Some(cur) = rt.invoke(m_get, component, &mut []) {
            if cur == st.os_font {
                return;
            }
        }
        let m_set = rt.find_method_in_hierarchy(k, "set_font", 1);
        let _ = rt.invoke(m_set, component, &mut [st.os_font]);
        return;
    }

    if !is_tmp_hook(hook_id) {
        return;
    }
    if st.bundle_font.is_null() && st.dyn_font.is_null() {
        return;
    }

    if st.t_tmp.is_null() {
        st.t_tmp = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Text");
    }
    if !st.t_tmp.is_null() {
        if st.f_font.is_null() {
            st.f_font = rt.find_field_in_hierarchy(st.t_tmp, "m_fontAsset");
        }
        if st.m_tmp_get_font.is_null() {
            st.m_tmp_get_font = rt.find_method_in_hierarchy(st.t_tmp, "get_font", 0);
        }
        if st.m_tmp_set_font.is_null() {
            st.m_tmp_set_font = rt.find_method_in_hierarchy(st.t_tmp, "set_font", 1);
        }
    }
    if st.t_fa.is_null() {
        st.t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    }
    let t_tmp = st.t_tmp;
    let t_fa = st.t_fa;
    if t_tmp.is_null() || t_fa.is_null() {
        return;
    }

    // Prefer user-selected OS dynamic font; fall back to bundle only if dynamic creation failed.
    let target = if !st.dyn_font.is_null() {
        st.dyn_font
    } else {
        st.bundle_font
    };

    let f_font = st.f_font;
    let mut cur_font: *mut c_void = std::ptr::null_mut();
    let mut comp_cls: *mut c_void = std::ptr::null_mut();
    if !f_font.is_null() {
        rt.field_get_value(component, f_font, &mut cur_font as *mut *mut c_void as *mut c_void);
    } else {
        let mut m_get = st.m_tmp_get_font;
        if m_get.is_null() {
            comp_cls = rt.object_get_class(component);
            m_get = rt.find_method_in_hierarchy(comp_cls, "get_font", 0);
        }
        if !m_get.is_null() {
            cur_font = rt.invoke(m_get, component, &mut []).unwrap_or(std::ptr::null_mut());
        }
    }

    if !cur_font.is_null()
        && cur_font != st.bundle_font
        && cur_font != st.dyn_font
        && st.fallback_attached.insert(cur_font as usize)
    {
        if st.fallback_attached.len() > MAX_SCALED_CACHE {
            st.fallback_attached.clear();
        }
        attach_fallback_to_asset(rt, t_fa, cur_font, st.dyn_font);
        attach_fallback_to_asset(rt, t_fa, cur_font, st.bundle_font);
    }

    if cur_font == target {
        return;
    }

    let mut m_set = st.m_tmp_set_font;
    if m_set.is_null() {
        if comp_cls.is_null() {
            comp_cls = rt.object_get_class(component);
        }
        m_set = rt.find_method_in_hierarchy(comp_cls, "set_font", 1);
    }
    if !m_set.is_null() {
        let _ = rt.invoke(m_set, component, &mut [target]);
    } else if !f_font.is_null() {
        if comp_cls.is_null() {
            comp_cls = rt.object_get_class(component);
        }
        rt.field_set_value(component, f_font, target);
        let f_changed = rt.find_field_in_hierarchy(t_tmp, "m_havePropertiesChanged");
        if !f_changed.is_null() {
            let mut b_true: u8 = 1;
            rt.field_set_value(component, f_changed, &mut b_true as *mut u8 as *mut c_void);
        }
        let mut m_upd = rt.find_method_in_hierarchy(comp_cls, "UpdateFontAsset", 0);
        if m_upd.is_null() {
            m_upd = rt.find_method_in_hierarchy(comp_cls, "LoadFontAsset", 0);
        }
        let _ = rt.invoke(m_upd, component, &mut []);
        let m_dirty = rt.find_method_in_hierarchy(comp_cls, "SetAllDirty", 0);
        let _ = rt.invoke(m_dirty, component, &mut []);
    }
}

fn scale_component_font(
    rt: &dyn UnityRuntime,
    st: &mut FontState,
    component: *mut c_void,
    hook_id: u64,
    scale: f32,
) {
    let key = component as usize;
    let comp_cls = rt.object_get_class(component);
    if comp_cls.is_null() {
        return;
    }
    let cls_key = comp_cls as usize;
    let (m_get, m_set) = if let Some(&pair) = st.size_methods.get(&cls_key) {
        pair
    } else {
        let pair = (
            rt.find_method_in_hierarchy(comp_cls, "get_fontSize", 0),
            rt.find_method_in_hierarchy(comp_cls, "set_fontSize", 1),
        );
        if st.size_methods.len() >= 128 {
            st.size_methods.clear();
        }
        st.size_methods.insert(cls_key, pair);
        pair
    };
    let Some(boxed) = rt.invoke(m_get, component, &mut []) else { return };
    let val_ptr = rt.unbox(boxed);
    if val_ptr.is_null() || m_set.is_null() {
        return;
    }

    if is_tmp_hook(hook_id) {
        let cur = unsafe { *(val_ptr as *const f32) };
        if cur > 0.0 {
            let cur_bits = cur.to_bits();
            if st.scaled.get(&key).copied() == Some(cur_bits) {
                return;
            }
            let mut next = (cur * scale).max(1.0);
            let _ = rt.invoke(m_set, component, &mut [&mut next as *mut f32 as *mut c_void]);
            if st.scaled.len() >= MAX_SCALED_CACHE && !st.scaled.contains_key(&key) {
                st.scaled.clear();
            }
            st.scaled.insert(key, next.to_bits());
        }
    } else if is_ui_text_hook(hook_id) || hook_id == HOOK_TEXTMESH_SET_TEXT {
        let cur = unsafe { *(val_ptr as *const i32) };
        if cur > 0 {
            let cur_bits = cur as u32;
            if st.scaled.get(&key).copied() == Some(cur_bits) {
                return;
            }
            let mut next = ((cur as f32) * scale).round().max(1.0) as i32;
            let _ = rt.invoke(m_set, component, &mut [&mut next as *mut i32 as *mut c_void]);
            if st.scaled.len() >= MAX_SCALED_CACHE && !st.scaled.contains_key(&key) {
                st.scaled.clear();
            }
            st.scaled.insert(key, next as u32);
        }
    }
}

fn read_i32_prop(rt: &dyn UnityRuntime, component: *mut c_void, getter: *mut c_void) -> Option<i32> {
    if getter.is_null() {
        return None;
    }
    let boxed = rt.invoke(getter, component, &mut [])?;
    let ptr = rt.unbox(boxed);
    if ptr.is_null() {
        return None;
    }
    Some(unsafe { *(ptr as *const i32) })
}

fn read_f32_prop(rt: &dyn UnityRuntime, component: *mut c_void, getter: *mut c_void) -> Option<f32> {
    if getter.is_null() {
        return None;
    }
    let boxed = rt.invoke(getter, component, &mut [])?;
    let ptr = rt.unbox(boxed);
    if ptr.is_null() {
        return None;
    }
    Some(unsafe { *(ptr as *const f32) })
}

fn read_u8_prop(rt: &dyn UnityRuntime, component: *mut c_void, getter: *mut c_void) -> Option<u8> {
    if getter.is_null() {
        return None;
    }
    let boxed = rt.invoke(getter, component, &mut [])?;
    let ptr = rt.unbox(boxed);
    if ptr.is_null() {
        return None;
    }
    Some(unsafe { *(ptr as *const u8) })
}

fn read_rect_width(
    rt: &dyn UnityRuntime,
    component: *mut c_void,
    m_get_rt: *mut c_void,
    m_get_rect: *mut c_void,
) -> Option<f32> {
    if m_get_rt.is_null() || m_get_rect.is_null() {
        return None;
    }
    let rt_obj = rt.invoke(m_get_rt, component, &mut [])?;
    if rt_obj.is_null() {
        return None;
    }
    let boxed = rt.invoke(m_get_rect, rt_obj, &mut [])?;
    let ptr = rt.unbox(boxed);
    if ptr.is_null() {
        return None;
    }
    let rect = unsafe { *(ptr as *const [f32; 4]) };
    Some(rect[2].abs())
}

fn ensure_text_layout(
    rt: &dyn UnityRuntime,
    st: &mut FontState,
    component: *mut c_void,
    hook_id: u64,
    text: &str,
) {
    let relax_multiline = matches!(embed::current_display_mode(), 1 | 2) || text.contains('\n');
    if !is_tmp_hook(hook_id) && !relax_multiline {
        return;
    }
    let comp_cls = rt.object_get_class(component);
    if comp_cls.is_null() {
        return;
    }
    let cls_key = comp_cls as usize;
    let methods = if let Some(&cached) = st.layout_methods.get(&cls_key) {
        cached
    } else {
        let resolved = if is_tmp_hook(hook_id) {
            let rt_cls = rt.find_class("UnityEngine.CoreModule", "UnityEngine", "RectTransform");
            let m_get_rect = if rt_cls.is_null() {
                std::ptr::null_mut()
            } else {
                rt.find_method_in_hierarchy(rt_cls, "get_rect", 0)
            };
            LayoutMethods {
                resolved: true,
                m_get_overflow: rt.find_method_in_hierarchy(comp_cls, "get_overflowMode", 0),
                m_set_overflow: rt.find_method_in_hierarchy(comp_cls, "set_overflowMode", 1),
                m_get_max_lines: rt.find_method_in_hierarchy(comp_cls, "get_maxVisibleLines", 0),
                m_set_max_lines: rt.find_method_in_hierarchy(comp_cls, "set_maxVisibleLines", 1),
                m_get_max_chars: rt.find_method_in_hierarchy(comp_cls, "get_maxVisibleCharacters", 0),
                m_set_max_chars: rt.find_method_in_hierarchy(comp_cls, "set_maxVisibleCharacters", 1),
                m_get_rect_transform: rt.find_method_in_hierarchy(comp_cls, "get_rectTransform", 0),
                m_get_rect,
                m_get_font_size: rt.find_method_in_hierarchy(comp_cls, "get_fontSize", 0),
                m_get_wrap_mode: rt.find_method_in_hierarchy(comp_cls, "get_textWrappingMode", 0),
                m_set_wrap_mode: rt.find_method_in_hierarchy(comp_cls, "set_textWrappingMode", 1),
                m_get_word_wrap: rt.find_method_in_hierarchy(comp_cls, "get_enableWordWrapping", 0),
                m_set_word_wrap: rt.find_method_in_hierarchy(comp_cls, "set_enableWordWrapping", 1),
                ..Default::default()
            }
        } else if is_ui_text_hook(hook_id) {
            LayoutMethods {
                resolved: true,
                m_get_v_overflow: rt.find_method_in_hierarchy(comp_cls, "get_verticalOverflow", 0),
                m_set_v_overflow: rt.find_method_in_hierarchy(comp_cls, "set_verticalOverflow", 1),
                ..Default::default()
            }
        } else {
            LayoutMethods { resolved: true, ..Default::default() }
        };
        if st.layout_methods.len() >= 128 {
            st.layout_methods.clear();
        }
        st.layout_methods.insert(cls_key, resolved);
        resolved
    };
    if !methods.resolved {
        return;
    }

    if is_tmp_hook(hook_id) {
        let visible_chars = embed::visible_char_count(text).min(i32::MAX as usize) as i32;
        if let Some(cur_chars) = read_i32_prop(rt, component, methods.m_get_max_chars) {
            if cur_chars > 0 && cur_chars < visible_chars && !methods.m_set_max_chars.is_null() {
                let mut unlimited: i32 = 99_999;
                let _ = rt.invoke(
                    methods.m_set_max_chars,
                    component,
                    &mut [&mut unlimited as *mut i32 as *mut c_void],
                );
            }
        }
        // Avoid 1-2 glyph per line wrapping when a speech bubble has not expanded its RectTransform yet.
        if visible_chars >= 6 {
            if let (Some(rect_w), Some(font_sz)) = (
                read_rect_width(rt, component, methods.m_get_rect_transform, methods.m_get_rect),
                read_f32_prop(rt, component, methods.m_get_font_size),
            ) {
                if rect_w > 0.5 && font_sz > 0.0 && rect_w < font_sz * 2.5 {
                    if !methods.m_set_wrap_mode.is_null() {
                        if matches!(read_i32_prop(rt, component, methods.m_get_wrap_mode), Some(1 | 2)) {
                            let mut no_wrap: i32 = 0;
                            let _ = rt.invoke(
                                methods.m_set_wrap_mode,
                                component,
                                &mut [&mut no_wrap as *mut i32 as *mut c_void],
                            );
                        }
                    } else if !methods.m_set_word_wrap.is_null()
                        && read_u8_prop(rt, component, methods.m_get_word_wrap) == Some(1)
                    {
                        let mut no_wrap: u8 = 0;
                        let _ = rt.invoke(
                            methods.m_set_word_wrap,
                            component,
                            &mut [&mut no_wrap as *mut u8 as *mut c_void],
                        );
                    }
                }
            }
        }
        if relax_multiline {
            if let Some(cur_overflow) = read_i32_prop(rt, component, methods.m_get_overflow) {
                // TextOverflowModes: 0=Overflow, 1=Ellipsis, 3=Truncate.
                if matches!(cur_overflow, 1 | 3) && !methods.m_set_overflow.is_null() {
                    let mut overflow_mode: i32 = 0;
                    let _ = rt.invoke(
                        methods.m_set_overflow,
                        component,
                        &mut [&mut overflow_mode as *mut i32 as *mut c_void],
                    );
                }
            }
            let line_count = text.lines().count().max(1) as i32;
            if let Some(cur_lines) = read_i32_prop(rt, component, methods.m_get_max_lines) {
                if cur_lines > 0 && cur_lines < line_count && !methods.m_set_max_lines.is_null() {
                    let mut unlimited: i32 = 99_999;
                    let _ = rt.invoke(
                        methods.m_set_max_lines,
                        component,
                        &mut [&mut unlimited as *mut i32 as *mut c_void],
                    );
                }
            }
        }
    } else if is_ui_text_hook(hook_id) && relax_multiline {
        if let Some(cur_v) = read_i32_prop(rt, component, methods.m_get_v_overflow) {
            // VerticalWrapMode: 0=Truncate, 1=Overflow.
            if cur_v == 0 && !methods.m_set_v_overflow.is_null() {
                let mut overflow: i32 = 1;
                let _ = rt.invoke(
                    methods.m_set_v_overflow,
                    component,
                    &mut [&mut overflow as *mut i32 as *mut c_void],
                );
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn resolve_font_paths_finds_windows_fonts() {
        let tahoma = resolve_font_paths("Tahoma");
        assert!(
            tahoma.first().is_some_and(|p| p.to_ascii_lowercase().ends_with("tahoma.ttf")),
            "expected tahoma.ttf first, got {tahoma:?}"
        );
        let arial = resolve_font_paths("Arial");
        assert!(
            arial.first().is_some_and(|p| p.to_ascii_lowercase().ends_with("arial.ttf")),
            "expected arial.ttf first, got {arial:?}"
        );
    }
}
