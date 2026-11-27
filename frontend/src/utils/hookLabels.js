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
  } else if (src.includes("unity")) {
    base = isAr ? "قناة نصوص Unity" : "Unity Text Hook";
  }

  return idx >= 0 ? `${base} #${idx + 1}` : base;
}
