import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { TRADITIONAL_PROVIDERS, ALL_PROVIDERS } from "./providers.js";
import { formatHookLabel } from "./hookLabels.js";
import { formatFriendlyError } from "./friendlyErrors.js";

describe("providers registry", () => {
  it("contains unique provider IDs across ALL_PROVIDERS", () => {
    const ids = ALL_PROVIDERS.map((p) => p.id);
    assert.equal(new Set(ids).size, ids.length);
  });

  it("includes all 13 traditional providers plus offline_llm and llm", () => {
    assert.equal(TRADITIONAL_PROVIDERS.length, 13);
    const ids = new Set(ALL_PROVIDERS.map((p) => p.id));
    for (const tp of TRADITIONAL_PROVIDERS) {
      assert.ok(ids.has(tp.id), `missing traditional provider ${tp.id}`);
    }
    assert.ok(ids.has("offline_llm"));
    assert.ok(ids.has("llm"));
  });
});

describe("formatHookLabel", () => {
  it("formats TMP, UI.Text, StaticUI, and TextAnimator hooks in EN and AR", () => {
    assert.equal(formatHookLabel({ hookCode: "UNITY:TMP_Text.set_text" }, -1, "en"), "Unity TextMeshPro");
    assert.equal(formatHookLabel({ hookCode: "UNITY:UI.Text.set_text" }, 0, "en"), "Unity UI Text #1");
    assert.equal(
      formatHookLabel({ hookCode: "UNITY:TMP.StaticUI" }, -1, "en"),
      "Static UI Menus & Labels (TMP)",
    );
    assert.equal(
      formatHookLabel({ hookCode: "UNITY:UI.Text.StaticUI" }, -1, "ar"),
      "نصوص القوائم الثابتة (Static UI Text)",
    );
    assert.equal(
      formatHookLabel({ hookCode: "UNITY:TextAnimator.SetText" }, -1, "en"),
      "Febucci TextAnimator",
    );
  });
});

describe("formatFriendlyError", () => {
  it("maps known error strings to friendly actions and passes unknown errors through", () => {
    assert.equal(formatFriendlyError("OpenProcess: Access is denied.", "en").action, "admin");
    assert.equal(formatFriendlyError("context deadline exceeded", "en").action, "benchmark");
    assert.equal(formatFriendlyError("HTTP 429 too many requests", "en").action, "benchmark");
    assert.equal(
      formatFriendlyError("update rejected: no SHA-256 digest available", "en").title,
      "Could not verify update integrity",
    );
    assert.equal(formatFriendlyError("some unexpected failure", "en").title, "some unexpected failure");
    assert.equal(formatFriendlyError("", "en").title, "");
  });
});
