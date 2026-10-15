import { describe, expect, it } from "vitest";
import { cssVariablesResolver, DARK_TOKENS } from "./dark";
import { TOKENS } from "./tokens";

function lum(hex: string): number {
  const n = parseInt(hex.slice(1), 16);
  const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
}
const contrast = (a: string, b: string) => {
  const [x, y] = [lum(a), lum(b)].sort((p, q) => q - p);
  return (x! + 0.05) / (y! + 0.05);
};
const d = (k: string) => DARK_TOKENS[k]!;

describe("тёмная тема", () => {
  it("переопределяет только существующие токены", () => {
    for (const k of Object.keys(DARK_TOKENS)) expect(TOKENS[k], k).toBeDefined();
  });

  it("разворачивается поверх светлой: общие размеры и шрифты те же", () => {
    const r = cssVariablesResolver();
    expect(r.dark["--card-pad"]).toBe(TOKENS["--card-pad"]);
    expect(r.dark["--text"]).toBe(DARK_TOKENS["--text"]);
    expect(r.light["--text"]).toBe(TOKENS["--text"]);
  });

  const pairs: [string, string][] = [
    ["--text", "--paper"], ["--text", "--surface"], ["--text-muted", "--paper"], ["--text-muted", "--surface"],
    ["--text-subtle", "--surface"], ["--brand-ink", "--paper"], ["--brand-ink", "--surface"],
    ["--primary", "--surface"], ["--on-primary", "--primary"], ["--on-primary", "--primary-hover"],
    ["--urgency-ok-fg", "--surface"], ["--urgency-warn-fg", "--urgency-warn-bg"], ["--urgency-risk-fg", "--urgency-risk-bg"],
    ["--urgency-critical-fg", "--urgency-critical-bg"], ["--urgency-neutral-fg", "--urgency-neutral-bg"],
    ["--urgency-info-fg", "--urgency-info-bg"], ["--urgency-warn-fg", "--urgency-warn-row"], ["--urgency-risk-fg", "--urgency-risk-row"],
    ["--text", "--row-changed"],
  ];
  for (const [fg, bg] of pairs) {
    it(`${fg} на ${bg} ≥ 4,5 : 1`, () => {
      expect(contrast(d(fg), d(bg))).toBeGreaterThanOrEqual(4.5);
    });
  }

  it("контур поля и кольцо фокуса различимы (≥ 3 : 1)", () => {
    expect(contrast(d("--border-strong"), d("--surface"))).toBeGreaterThanOrEqual(3);
    expect(contrast(d("--focus-ring"), d("--surface"))).toBeGreaterThanOrEqual(3);
    expect(contrast(d("--focus-ring"), d("--paper"))).toBeGreaterThanOrEqual(3);
  });
});
