import { MESSAGES } from "../i18n/messages.js";

export function formatFriendlyError(raw, lang = "ar") {
  const dict = MESSAGES[lang] || MESSAGES.ar;
  const rawStr = String(raw || "").trim();
  if (!rawStr) return { title: "", hint: "", action: "" };

  const low = rawStr.toLowerCase();

  if (low.includes("access is denied") || low.includes("openprocess") || low.includes("privilege") || low.includes("elevation")) {
    return { title: dict.errAdminTitle, hint: dict.errAdminHint, action: "admin", raw: rawStr };
  }
  if (low.includes("429") || low.includes("rate limit") || low.includes("too many requests")) {
    return { title: dict.errRateTitle, hint: dict.errRateHint, action: "benchmark", raw: rawStr };
  }
  if (low.includes("timeout") || low.includes("deadline exceeded") || low.includes("timed out")) {
    return { title: dict.errTimeoutTitle, hint: dict.errTimeoutHint, action: "benchmark", raw: rawStr };
  }
  if (low.includes("no translator") || low.includes("no provider")) {
    return { title: dict.errNoProviderTitle, hint: dict.errNoProviderHint, action: "benchmark", raw: rawStr };
  }
  if (low.includes("32-bit") || low.includes("x86")) {
    return { title: dict.err32BitTitle, hint: dict.err32BitHint, action: "", raw: rawStr };
  }
  if (low.includes("unsupported engine")) {
    return { title: dict.errEngineTitle, hint: dict.errEngineHint, action: "", raw: rawStr };
  }
  if (low.includes("font") && (low.includes("sha-256") || low.includes("download") || low.includes("bundle"))) {
    return { title: dict.errFontsTitle, hint: dict.errFontsHint, action: "fonts", raw: rawStr };
  }
  if (low.includes("hook host unavailable")) {
    return { title: dict.errHostTitle, hint: dict.errHostHint, action: "", raw: rawStr };
  }

  return { title: rawStr, hint: "", action: "", raw: rawStr };
}
