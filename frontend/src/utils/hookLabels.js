export function formatHookLabel(h, idx = -1, lang = "en") {
  const src = `${h?.hookCode || ""} ${h?.hookName || ""}`.toLowerCase();
  const isAr = lang === "ar";
  let base = h?.hookName || h?.hookCode || (isAr ? "قناة نصية" : "Text Hook");

  if (src.includes("staticui")) {
    if (src.includes("ui.text")) {
      base = isAr ? "نصوص القوائم الثابتة (Static UI Text)" : "Static UI Menus & Labels (UI Text)";
    } else {
      base = isAr ? "نصوص القوائم الثابتة (Static TMP UI)" : "Static UI Menus & Labels (TMP)";
    }
  } else if (src.includes("textmeshprougui")) {
    base = isAr ? "نصوص الواجهة والحوار (TextMeshPro UI)" : "Unity TextMeshPro UI";
  } else if (src.includes("textmeshpro") || src.includes("tmp_text")) {
    base = isAr ? "نصوص اللعبة (TextMeshPro)" : "Unity TextMeshPro";
  } else if (src.includes("unityengine.ui") || src.includes("ui.text")) {
    base = isAr ? "نصوص Unity القياسية (UI Text)" : "Unity UI Text";
  } else if (src.includes("uielements")) {
    base = isAr ? "واجهة Unity الحديثة (UI Toolkit)" : "Unity UI Toolkit";
  } else if (src.includes("uilabel")) {
    base = isAr ? "نصوص NGUI (UILabel)" : "Unity NGUI Label";
  } else if (src.includes("naninovel")) {
    base = isAr ? "حوارات Naninovel" : "Naninovel Dialogue";
  } else if (src.includes("textanimator")) {
    base = isAr ? "حوارات TextAnimator (Febucci)" : "Febucci TextAnimator";
  } else if (src.includes("unity")) {
    base = isAr ? "قناة نصوص Unity" : "Unity Text Hook";
  }

  return idx >= 0 ? `${base} #${idx + 1}` : base;
}

/**
 * Formats a Unity GameObject hierarchy path into a compact, readable label
 * (e.g. "Canvas/DialoguePanel/BodyText" -> "DialoguePanel › BodyText").
 */
export function formatObjectPath(path) {
  const raw = String(path || "").trim();
  if (!raw) return "";
  const segs = raw
    .split("/")
    .map((s) => s.trim())
    .filter(Boolean);
  if (!segs.length) return "";
  if (segs.length > 2 && /^canvas$/i.test(segs[0])) {
    return segs.slice(1).join(" › ");
  }
  return segs.slice(-3).join(" › ");
}

/**
 * Returns the primary human-friendly display title for a hook, preferring
 * Unity GameObject path or saved alias when available.
 */
export function formatHookPrimaryTitle(h, lang = "en") {
  if (h?.label) return h.label;
  const objTitle = formatObjectPath(h?.objectPath || h?.path);
  if (objTitle) return objTitle;
  return formatHookLabel(h, -1, lang);
}

/**
 * Returns localized display metadata for a classified hook category.
 */
export function getHookCategoryMeta(category, lang = "en") {
  const isAr = lang === "ar";
  switch (category) {
    case "dialogue":
      return { id: "dialogue", label: isAr ? "حوار" : "Dialogue", tone: "dialogue" };
    case "speaker":
      return { id: "speaker", label: isAr ? "متحدث" : "Speaker", tone: "speaker" };
    case "ui":
      return { id: "ui", label: isAr ? "واجهة" : "UI", tone: "ui" };
    case "noise":
      return { id: "noise", label: isAr ? "ضوضاء" : "Noise", tone: "noise" };
    default:
      return { id: "unknown", label: isAr ? "نص" : "Text", tone: "unknown" };
  }
}

/**
 * Formats a compact relative timestamp (e.g. "now", "4s", "2m").
 */
export function formatRelativeAgo(atMs, nowMs = Date.now(), lang = "en") {
  if (!atMs || atMs <= 0) return "";
  const sec = Math.max(0, Math.floor((nowMs - atMs) / 1000));
  const isAr = lang === "ar";
  if (sec <= 2) return isAr ? "الآن" : "now";
  if (sec < 60) return isAr ? `منذ ${sec}ث` : `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return isAr ? `منذ ${min}د` : `${min}m ago`;
  const hr = Math.floor(min / 60);
  return isAr ? `منذ ${hr}س` : `${hr}h ago`;
}
