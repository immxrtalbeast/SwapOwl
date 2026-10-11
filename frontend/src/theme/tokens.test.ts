import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { TOKENS } from "./tokens";

const css = readFileSync(new URL("../../../docs/design/tokens.css", import.meta.url), "utf8");

describe("tokens.ts совпадает с docs/design/tokens.css", () => {
  const expected = new Map<string, string>();
  for (const m of css.replace(/\/\*[\s\S]*?\*\//g, "").matchAll(/(--[\w-]+):([^;]+);/g)) expected.set(m[1], m[2].trim());

  it("каждая переменная файла есть в коде с тем же значением", () => {
    expect(expected.size).toBeGreaterThan(60);
    for (const [k, v] of expected) expect(TOKENS[k], k).toBe(v);
  });
  it("в коде нет лишних переменных", () => {
    for (const k of Object.keys(TOKENS)) expect(expected.has(k), k).toBe(true);
  });
});
