import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { TOKENS } from "../theme/tokens";
import { contrast } from "./contrast";

const t = (k: string) => TOKENS[k];

// Пары «текст на фоне», которые реально встречаются в интерфейсе (frontend-prompt.md, «Цвета и токены»).
const TEXT_PAIRS: Array<[string, string, string]> = [
  ["--text", "--paper", "основной текст на странице"],
  ["--text", "--surface", "основной текст в карточке"],
  ["--text-muted", "--surface", "вторичный текст"],
  ["--text-muted", "--surface-sunken", "шапка таблицы"],
  ["--text-muted", "--paper", "вторичный текст на странице"],
  ["--text-subtle", "--surface", "приглушённый текст в карточке"],
  ["--text-muted", "--paper", "подвал"],
  ["--primary", "--surface", "ссылка и текстовая кнопка"],
  ["--brand-ink", "--paper", "текстовая кнопка и ссылка на странице"],
  ["--on-primary", "--primary", "основная кнопка и боковая панель"],
  ["--on-primary", "--primary-hover", "кнопка при наведении"],
  ["--brand-ink", "--brand-tint", "плашка"],
  ["--urgency-ok-fg", "--urgency-ok-bg", "бейдж «ок»"],
  ["--urgency-warn-fg", "--urgency-warn-bg", "бейдж «истекает»"],
  ["--urgency-warn-fg", "--urgency-warn-row", "строка «истекает»"],
  ["--urgency-risk-fg", "--urgency-risk-bg", "бейдж «спишется»"],
  ["--urgency-risk-fg", "--urgency-risk-row", "строка «спишется»"],
  ["--urgency-critical-fg", "--urgency-critical-bg", "бейдж «просрочена»"],
  ["--urgency-critical-fg", "--surface", "ошибка на карточке"],
  ["--urgency-critical-fg", "--paper", "ошибка на странице"],
  ["--urgency-neutral-fg", "--urgency-neutral-bg", "бейдж «израсходована»"],
  ["--urgency-info-fg", "--urgency-info-bg", "бейдж «по плану»"],
  ["--on-urgency-warn-solid", "--urgency-warn-solid", "счётчик «истекают»"],
  ["--on-urgency-solid", "--urgency-risk-solid", "счётчик «спишутся»"],
  ["--on-urgency-solid", "--urgency-critical-solid", "счётчик «просрочено»"],
  ["--on-urgency-solid", "--urgency-ok-solid", "счётчик «ок»"],
];

describe("контраст текста ≥ 4,5 : 1", () => {
  for (const [fg, bg, where] of TEXT_PAIRS) {
    it(`${fg} на ${bg} — ${where}`, () => {
      expect(contrast(t(fg), t(bg))).toBeGreaterThanOrEqual(4.5);
    });
  }
});

describe("нетекстовые элементы ≥ 3 : 1", () => {
  it("контур поля на карточке", () => expect(contrast(t("--border-strong"), t("--surface"))).toBeGreaterThanOrEqual(3));
  it("кольцо фокуса на странице и карточке", () => {
    expect(contrast(t("--focus-ring"), t("--paper"))).toBeGreaterThanOrEqual(3);
    expect(contrast(t("--focus-ring"), t("--surface"))).toBeGreaterThanOrEqual(3);
  });
});

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    if (statSync(p).isDirectory()) return sources(p);
    return p.endsWith(".tsx") && !p.endsWith(".test.tsx") ? [p] : [];
  });
}
const srcRoot = new URL("..", import.meta.url).pathname;
const files = sources(srcRoot).map((p) => ({ p: p.slice(srcRoot.length), s: readFileSync(p, "utf8") }));

describe("подписи у полей", () => {
  it("каждое нативное поле ввода имеет подпись: обёртка <label> или aria-label", () => {
    const bad: string[] = [];
    for (const { p, s } of files) {
      for (const m of s.matchAll(/<(input|select|textarea)\b[^>]*>/g)) {
        const before = s.slice(0, m.index);
        const insideLabel = before.lastIndexOf("<label") > before.lastIndexOf("</label>");
        if (!insideLabel && !/aria-label(ledby)?=/.test(m[0])) bad.push(`${p}: ${m[0].slice(0, 60)}`);
      }
    }
    expect(bad).toEqual([]);
  });
  it("поля Mantine имеют label", () => {
    const bad: string[] = [];
    for (const { p, s } of files) {
      for (const m of s.matchAll(/<(TextInput|PasswordInput|NumberInput|Select|Textarea)\b[^>]*?\/?>/g)) {
        if (!/\blabel=/.test(m[0]) && !/aria-label=/.test(m[0])) bad.push(`${p}: ${m[0].slice(0, 60)}`);
      }
    }
    expect(bad).toEqual([]);
  });
});

describe("узкие экраны", () => {
  it("таблицы рисует только DataTable или обёртка с overflowX", () => {
    const bad = files.filter(({ s }) => /<table\b/.test(s) && !/overflowX:\s*"auto"/.test(s)).map((f) => f.p);
    expect(bad).toEqual([]);
  });
  it("в рабочих экранах нет фиксированной ширины ≥ 400 px", () => {
    const bad = files
      .filter(({ p }) => p.startsWith("pages/") || p.startsWith("components/"))
      .filter(({ s }) => /\b(width|minWidth):\s*(\d{3,})\b/.test(s) && [...s.matchAll(/\b(?:width|minWidth):\s*(\d{3,})\b/g)].some((m) => Number(m[1]) >= 400))
      .map((f) => f.p);
    expect(bad).toEqual([]);
  });
  it("в каркасе есть ссылка «К содержимому» и цель main#so-main", () => {
    const shell = files.find((f) => f.p === "app/AppShell.tsx")!.s;
    expect(shell).toContain('href="#so-main"');
    expect(shell).toContain('id="so-main"');
  });
});
