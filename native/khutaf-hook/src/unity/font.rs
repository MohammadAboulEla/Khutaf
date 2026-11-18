//! Unity font injection (TMP asset bundles + OS dynamic fonts) and size scaling.
//! Runs exclusively on the Unity main thread from inside hooked text setters.

use std::collections::{HashMap, HashSet};
use std::ffi::c_void;
use std::path::Path;
use std::sync::{LazyLock, Mutex};

use khutaf_proto::level;

use super::{
    is_tmp_hook, UnityRuntime, HOOK_TEXTMESH_SET_TEXT, HOOK_UI_TEXT_SET_TEXT,
};
use crate::embed;

const MAX_SCALED_CACHE: usize = 4096;

struct FontState {
    initialized: bool,
    loaded_dir: String,
    loaded_family: String,
    bundle_font: *mut c_void,
    dyn_font: *mut c_void,
    os_font: *mut c_void,
    tried_tmp_retry: bool,
    scaled: HashSet<usize>,
    bundle_checked: HashMap<u32, bool>,
    fallback_attached: HashSet<usize>,
}

unsafe impl Send for FontState {}

static STATE: LazyLock<Mutex<FontState>> = LazyLock::new(|| {
    Mutex::new(FontState {
        initialized: false,
        loaded_dir: String::new(),
        loaded_family: String::new(),
        bundle_font: std::ptr::null_mut(),
        dyn_font: std::ptr::null_mut(),
        os_font: std::ptr::null_mut(),
        tried_tmp_retry: false,
        scaled: HashSet::new(),
        bundle_checked: HashMap::new(),
        fallback_attached: HashSet::new(),
    })
});

pub fn on_text(rt: &dyn UnityRuntime, component: *mut c_void, hook_id: u64, text: &str) {
    let settings = embed::settings();
    let dir = settings.font_dir.trim().to_owned();
    let family = settings.font_family.trim().to_owned();

    let mut st = STATE.lock().unwrap_or_else(|p| p.into_inner());

    if !st.initialized || st.loaded_dir != dir || st.loaded_family != family {
        reload_fonts(rt, &mut st, &dir, &family, component, hook_id);
    } else if st.dyn_font.is_null()
        && !st.os_font.is_null()
        && !st.tried_tmp_retry
        && is_tmp_hook(hook_id)
        && !component.is_null()
    {
        // Retry TMP font creation once we see an actual TMP component that can donate its shader.
        st.tried_tmp_retry = true;
        if let Some(donor) = read_component_tmp_font(rt, component) {
            let dyn_tmp = create_tmp_font_asset(rt, st.os_font, donor);
            if !dyn_tmp.is_null() {
                rt.gchandle_pin(dyn_tmp);
                dont_destroy(rt, dyn_tmp);
                st.dyn_font = dyn_tmp;
                let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
                if !t_fa.is_null() {
                    if !st.bundle_font.is_null() && st.bundle_font != st.dyn_font {
                        attach_fallback_to_asset(rt, t_fa, st.bundle_font, st.dyn_font);
                    }
                    register_global_fallbacks(rt, t_fa, st.bundle_font, st.dyn_font);
                }
                crate::log(level::INFO, "unity TMP dynamic font created via component shader donor");
            }
        }
    }

    if !component.is_null() {
        apply_component_font(rt, &mut st, component, hook_id, text);
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
    let dir_changed = !st.initialized || st.loaded_dir != dir;
    let fam_changed = !st.initialized || st.loaded_family != family;
    st.initialized = true;
    st.loaded_dir = dir.to_owned();
    st.loaded_family = family.to_owned();
    st.tried_tmp_retry = false;
    st.scaled.clear();
    if dir_changed {
        st.bundle_checked.clear();
    }
    st.fallback_attached.clear();

    if dir.is_empty() && family.is_empty() {
        st.bundle_font = std::ptr::null_mut();
        st.dyn_font = std::ptr::null_mut();
        st.os_font = std::ptr::null_mut();
        return;
    }

    if dir_changed {
        st.bundle_font = if dir.is_empty() {
            std::ptr::null_mut()
        } else {
            load_bundle_font(rt, dir)
        };
    }

    if fam_changed {
        let donor = if !st.bundle_font.is_null() {
            st.bundle_font
        } else if is_tmp_hook(hook_id) {
            read_component_tmp_font(rt, component).unwrap_or(std::ptr::null_mut())
        } else {
            std::ptr::null_mut()
        };
        let (os, dyn_tmp) = load_os_fonts(rt, family, donor);
        st.os_font = os;
        st.dyn_font = dyn_tmp;
    }

    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if !t_fa.is_null() {
        if !st.bundle_font.is_null() && !st.dyn_font.is_null() && st.bundle_font != st.dyn_font {
            attach_fallback_to_asset(rt, t_fa, st.bundle_font, st.dyn_font);
        }
        register_global_fallbacks(rt, t_fa, st.bundle_font, st.dyn_font);
    }

    crate::log(
        level::INFO,
        format!(
            "unity font ready (bundle={}, os_tmp={}, os_ui={})",
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

fn load_bundle_font(rt: &dyn UnityRuntime, dir: &str) -> *mut c_void {
    let ver = unity_version(rt);
    let path = Path::new(dir).join(pick_bundle(&ver));
    if !path.exists() {
        crate::log(level::WARN, format!("TMP bundle missing: {}", path.display()));
        return std::ptr::null_mut();
    }
    let Some(path_str_arg) = path.to_str() else { return std::ptr::null_mut() };
    let path_managed = rt.string_new(path_str_arg);
    if path_managed.is_null() {
        return std::ptr::null_mut();
    }

    let t_ab = rt.find_class("UnityEngine.AssetBundleModule", "UnityEngine", "AssetBundle");
    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if t_fa.is_null() {
        return std::ptr::null_mut();
    }

    let mut ab = std::ptr::null_mut();
    if !t_ab.is_null() {
        let m_load = rt.find_method_param0(t_ab, "LoadFromFile", 1, "String");
        if let Some(res) = rt.invoke(m_load, std::ptr::null_mut(), &mut [path_managed]) {
            ab = res;
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
        return std::ptr::null_mut();
    }
    rt.gchandle_pin(ab);
    dont_destroy(rt, ab);

    let mut arr = std::ptr::null_mut();
    if !t_ab.is_null() {
        let m_all = rt.find_method(t_ab, "LoadAllAssets", 0);
        if let Some(res) = rt.invoke(m_all, ab, &mut []) {
            arr = res;
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
            rt.gchandle_pin(elem);
            dont_destroy(rt, elem);
            return elem;
        }
    }
    std::ptr::null_mut()
}

fn load_os_fonts(
    rt: &dyn UnityRuntime,
    family: &str,
    donor_font: *mut c_void,
) -> (*mut c_void, *mut c_void) {
    let t_font = rt.find_class("UnityEngine.TextRenderingModule", "UnityEngine", "Font");
    if t_font.is_null() {
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }

    let names: Vec<&str> = if family.is_empty() {
        vec!["Tahoma", "Arial", "Segoe UI"]
    } else {
        vec![family, "Tahoma", "Arial", "Segoe UI"]
    };
    let names_arr = rt.string_array_new(&names);

    let mut os_font = std::ptr::null_mut();

    // Try Internal_CreateDynamicFont icall first (IL2CPP) so CreateDynamicFontFromOSFont(string, int)
    // vs (string[], int) overload resolution cannot collide.
    if !names_arr.is_null() {
        let icall = rt.resolve_icall("UnityEngine.Font::Internal_CreateDynamicFont");
        if !icall.is_null() {
            let obj = rt.object_new(t_font);
            if !obj.is_null() {
                let f: unsafe extern "C" fn(*mut c_void, *mut c_void, i32) =
                    unsafe { std::mem::transmute_copy(&icall) };
                unsafe { f(obj, names_arr, 32) };
                os_font = obj;
            }
        }
    }

    if os_font.is_null() && !names_arr.is_null() {
        let m_dyn_arr = rt.find_method_param0(t_font, "CreateDynamicFontFromOSFont", 2, "String[]");
        if !m_dyn_arr.is_null() {
            let mut size: i32 = 32;
            let mut args = [names_arr, &mut size as *mut i32 as *mut c_void];
            if let Some(res) = rt.invoke(m_dyn_arr, std::ptr::null_mut(), &mut args) {
                os_font = res;
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
                    os_font = res;
                }
            }
        }
    }

    if os_font.is_null() {
        let icall = rt.resolve_icall("UnityEngine.Font::Internal_CreateFontFromPath");
        if !icall.is_null() {
            let f: unsafe extern "C" fn(*mut c_void, *mut c_void) =
                unsafe { std::mem::transmute_copy(&icall) };
            for ttf in [
                r"C:\Windows\Fonts\tahoma.ttf",
                r"C:\Windows\Fonts\arial.ttf",
                r"C:\Windows\Fonts\segoeui.ttf",
            ] {
                if !Path::new(ttf).exists() {
                    continue;
                }
                let obj = rt.object_new(t_font);
                if obj.is_null() {
                    break;
                }
                unsafe { f(obj, rt.string_new(ttf)) };
                os_font = obj;
                break;
            }
        }
    }

    if os_font.is_null() {
        return (std::ptr::null_mut(), std::ptr::null_mut());
    }
    rt.gchandle_pin(os_font);
    dont_destroy(rt, os_font);

    let dyn_tmp = create_tmp_font_asset(rt, os_font, donor_font);
    if !dyn_tmp.is_null() {
        rt.gchandle_pin(dyn_tmp);
        dont_destroy(rt, dyn_tmp);
    }
    (os_font, dyn_tmp)
}

fn ensure_mobile_sdf_shader(rt: &dyn UnityRuntime, t_fa: *mut c_void, donor_font: *mut c_void) {
    let t_shader_util = rt.find_class("Unity.TextMeshPro", "TMPro", "ShaderUtilities");
    if t_shader_util.is_null() {
        return;
    }
    rt.class_init(t_shader_util);

    let m_props = rt.find_method(t_shader_util, "GetShaderPropertyIDs", 0);
    if !m_props.is_null() {
        let _ = rt.invoke(m_props, std::ptr::null_mut(), &mut []);
    }

    let m_get_shader = rt.find_method(t_shader_util, "get_ShaderRef_MobileSDF", 0);
    if let Some(sh) = rt.invoke(m_get_shader, std::ptr::null_mut(), &mut []) {
        if !sh.is_null() {
            return;
        }
    }

    let mut donor = donor_font;
    if donor.is_null() {
        let t_settings = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Settings");
        let m_def = rt.find_method(t_settings, "get_defaultFontAsset", 0);
        if let Some(d) = rt.invoke(m_def, std::ptr::null_mut(), &mut []) {
            donor = d;
        }
    }
    if donor.is_null() {
        return;
    }

    let mut mat_obj: *mut c_void = std::ptr::null_mut();
    let donor_cls = rt.object_get_class(donor);
    let m_get_mat = rt.find_method_in_hierarchy(donor_cls, "get_material", 0);
    if let Some(m) = rt.invoke(m_get_mat, donor, &mut []) {
        mat_obj = m;
    }
    if mat_obj.is_null() {
        let mut f_mat = rt.find_field_in_hierarchy(t_fa, "material");
        if f_mat.is_null() {
            f_mat = rt.find_field_in_hierarchy(t_fa, "m_Material");
        }
        rt.field_get_value(donor, f_mat, &mut mat_obj as *mut *mut c_void as *mut c_void);
    }
    if mat_obj.is_null() {
        return;
    }

    let mut shader = std::ptr::null_mut();
    let icall_sh = rt.resolve_icall("UnityEngine.Material::get_shader");
    if !icall_sh.is_null() {
        let f: unsafe extern "C" fn(*mut c_void) -> *mut c_void = unsafe { std::mem::transmute_copy(&icall_sh) };
        shader = unsafe { f(mat_obj) };
    } else {
        let t_mat = rt.object_get_class(mat_obj);
        let m_sh = rt.find_method(t_mat, "get_shader", 0);
        if let Some(s) = rt.invoke(m_sh, mat_obj, &mut []) {
            shader = s;
        }
    }
    if !shader.is_null() {
        let f_ref = rt.field_from_name(t_shader_util, "k_ShaderRef_MobileSDF");
        rt.field_static_set_value(f_ref, shader);
    }
}

fn create_tmp_font_asset(
    rt: &dyn UnityRuntime,
    os_font: *mut c_void,
    donor_font: *mut c_void,
) -> *mut c_void {
    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if t_fa.is_null() {
        return std::ptr::null_mut();
    }
    ensure_mobile_sdf_shader(rt, t_fa, donor_font);

    let m1 = rt.find_method_param0(t_fa, "CreateFontAsset", 1, "Font");
    if !m1.is_null() {
        if let Some(fa) = rt.invoke(m1, std::ptr::null_mut(), &mut [os_font]) {
            if !fa.is_null() {
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
    if !m9.is_null() {
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
            if !fa.is_null() {
                return fa;
            }
        }
    }

    let m8 = rt.find_method_param0(t_fa, "CreateFontAsset", 8, "Font");
    if !m8.is_null() {
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
            if !fa.is_null() {
                return fa;
            }
        }
    }

    let m7 = rt.find_method_param0(t_fa, "CreateFontAsset", 7, "Font");
    if !m7.is_null() {
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
            if !fa.is_null() {
                return fa;
            }
        }
    }
    std::ptr::null_mut()
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

fn has_all_non_ascii(
    rt: &dyn UnityRuntime,
    t_fa: *mut c_void,
    font_asset: *mut c_void,
    cache: &mut HashMap<u32, bool>,
    text: &str,
) -> bool {
    let mut m_has: *mut c_void = std::ptr::null_mut();
    for ch in text.chars() {
        let code = ch as u32;
        if code < 0x0080 || code > 0xFFFF {
            continue;
        }
        if let Some(&ok) = cache.get(&code) {
            if !ok {
                return false;
            }
            continue;
        }
        if m_has.is_null() {
            m_has = rt.find_method_in_hierarchy(t_fa, "HasCharacter", 1);
            if m_has.is_null() {
                return true;
            }
        }
        let mut c16 = code as u16;
        let mut args = [&mut c16 as *mut u16 as *mut c_void];
        let Some(boxed) = rt.invoke(m_has, font_asset, &mut args) else {
            return true;
        };
        let ptr = rt.unbox(boxed) as *const u8;
        let ok = ptr.is_null() || unsafe { *ptr } != 0;
        if cache.len() < MAX_SCALED_CACHE {
            cache.insert(code, ok);
        }
        if !ok {
            return false;
        }
    }
    true
}

fn apply_component_font(
    rt: &dyn UnityRuntime,
    st: &mut FontState,
    component: *mut c_void,
    hook_id: u64,
    text: &str,
) {
    if hook_id == HOOK_UI_TEXT_SET_TEXT {
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

    let t_tmp = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_Text");
    let t_fa = rt.find_class("Unity.TextMeshPro", "TMPro", "TMP_FontAsset");
    if t_tmp.is_null() || t_fa.is_null() {
        return;
    }

    let mut target = if !st.bundle_font.is_null() { st.bundle_font } else { st.dyn_font };
    if !st.bundle_font.is_null()
        && !st.dyn_font.is_null()
        && !has_all_non_ascii(rt, t_fa, st.bundle_font, &mut st.bundle_checked, text)
    {
        target = st.dyn_font;
    }

    let comp_cls = rt.object_get_class(component);
    let m_get = rt.find_method_in_hierarchy(comp_cls, "get_font", 0);
    let f_font = rt.find_field_in_hierarchy(t_tmp, "m_fontAsset");
    let mut cur_font: *mut c_void = std::ptr::null_mut();
    if !m_get.is_null() {
        cur_font = rt.invoke(m_get, component, &mut []).unwrap_or(std::ptr::null_mut());
    } else if !f_font.is_null() {
        rt.field_get_value(component, f_font, &mut cur_font as *mut *mut c_void as *mut c_void);
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

    let m_set = rt.find_method_in_hierarchy(comp_cls, "set_font", 1);
    if !m_set.is_null() {
        let _ = rt.invoke(m_set, component, &mut [target]);
    } else if !f_font.is_null() {
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
    if st.scaled.len() >= MAX_SCALED_CACHE {
        st.scaled.clear();
    }
    if !st.scaled.insert(component as usize) {
        return;
    }
    let comp_cls = rt.object_get_class(component);
    if comp_cls.is_null() {
        return;
    }
    let m_get = rt.find_method_in_hierarchy(comp_cls, "get_fontSize", 0);
    let m_set = rt.find_method_in_hierarchy(comp_cls, "set_fontSize", 1);
    let Some(boxed) = rt.invoke(m_get, component, &mut []) else { return };
    let val_ptr = rt.unbox(boxed);
    if val_ptr.is_null() || m_set.is_null() {
        return;
    }

    if is_tmp_hook(hook_id) {
        let cur = unsafe { *(val_ptr as *const f32) };
        if cur > 0.0 {
            let mut next = (cur * scale).max(1.0);
            let _ = rt.invoke(m_set, component, &mut [&mut next as *mut f32 as *mut c_void]);
        }
    } else if hook_id == HOOK_UI_TEXT_SET_TEXT || hook_id == HOOK_TEXTMESH_SET_TEXT {
        let cur = unsafe { *(val_ptr as *const i32) };
        if cur > 0 {
            let mut next = ((cur as f32) * scale).round().max(1.0) as i32;
            let _ = rt.invoke(m_set, component, &mut [&mut next as *mut i32 as *mut c_void]);
        }
    }
}
