import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { MESSAGES } from "./messages.js";

const extractVars = (s) => (String(s).match(/\{(\w+)\}/g) || []).sort().join(",");

describe("i18n parity", () => {
  const arKeys = Object.keys(MESSAGES.ar);
  const enKeys = Object.keys(MESSAGES.en);

  it("has the exact same keys in ar and en", () => {
    const missingInEn = arKeys.filter((k) => !(k in MESSAGES.en));
    const missingInAr = enKeys.filter((k) => !(k in MESSAGES.ar));
    assert.deepEqual({ missingInEn, missingInAr }, { missingInEn: [], missingInAr: [] });
  });

  it("uses the same {placeholders} in both languages", () => {
    const mismatched = arKeys.filter(
      (k) => k in MESSAGES.en && extractVars(MESSAGES.ar[k]) !== extractVars(MESSAGES.en[k]),
    );
    assert.deepEqual(mismatched, []);
  });

  it("has no empty strings in either language", () => {
    for (const lang of ["ar", "en"]) {
      const empty = Object.entries(MESSAGES[lang])
        .filter(([, v]) => String(v).trim() === "")
        .map(([k]) => k);
      assert.deepEqual(empty, [], `empty strings in ${lang}`);
    }
  });
});
