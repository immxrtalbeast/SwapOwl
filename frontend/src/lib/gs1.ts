/** Разбор GS1 Element String (GS1-128, GS1 DataMatrix, DataMatrix «Честного знака»). */

const GS = "\x1d";

/** Фиксированная длина значения AI; остальные — переменной длины с максимумом. */
const FIXED: Record<string, number> = { "00": 18, "01": 14, "02": 14, "11": 6, "12": 6, "13": 6, "15": 6, "16": 6, "17": 6, "20": 2 };
const VAR_MAX: Record<string, number> = { "10": 20, "21": 20, "22": 20, "30": 8, "37": 8, "240": 30, "241": 30, "242": 6 };

export interface Gs1 {
  /** Все элементы: AI → значение (последний при повторе). */
  ai: Record<string, string>;
  gtin?: string;
  batch?: string;
  serial?: string;
  /** Дата производства, YYYY-MM-DD. */
  producedOn?: string;
  /** Годен до, YYYY-MM-DD (AI 17; день 00 = последний день месяца). */
  expiresOn?: string;
  /** Вес нетто, кг (AI 310n) */
  weightKg?: number;
}

/** AI 11/17 YYMMDD → YYYY-MM-DD; DD=00 — последний день месяца. Год: 00–99 → 2000–2099. */
export function parseGs1Date(s: string): string | undefined {
  if (!/^\d{6}$/.test(s)) return undefined;
  const y = 2000 + Number(s.slice(0, 2));
  const m = Number(s.slice(2, 4));
  let d = Number(s.slice(4, 6));
  if (m < 1 || m > 12) return undefined;
  const last = new Date(Date.UTC(y, m, 0)).getUTCDate();
  if (d === 0) d = last;
  if (d > last) return undefined;
  return `${y}-${String(m).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

function aiLength(s: string, i: number): string | undefined {
  const two = s.slice(i, i + 2);
  if (two in FIXED || two in VAR_MAX) return two;
  if (two === "31" && /^31[0-5]\d/.test(s.slice(i, i + 4))) return s.slice(i, i + 4);
  const three = s.slice(i, i + 3);
  if (three in VAR_MAX) return three;
  if (/^9[1-9]$/.test(two)) return two;
  return undefined;
}

/** Скобочная запись «(01)…(17)…» → строка с GS между элементами. */
function fromParens(s: string): string {
  const parts = [...s.matchAll(/\((\d{2,4})\)([^(]*)/g)];
  return parts.map((m) => m[1] + m[2]).join(GS);
}

/** Возвращает null, если строка не похожа на GS1 (нет GTIN и ни одного распознанного AI). */
export function parseGs1(raw: string): Gs1 | null {
  let s = raw.trim().replace(/^\](?:d2|C1|Q3|e0)/, "");
  if (/^\(\d{2,4}\)/.test(s)) s = fromParens(s);
  s = s.replace(/␝|<GS>/g, GS);
  const ai: Record<string, string> = {};
  let i = 0;
  while (i < s.length) {
    if (s[i] === GS) {
      i++;
      continue;
    }
    const code = aiLength(s, i);
    if (!code) return Object.keys(ai).length ? finish(ai) : null;
    i += code.length;
    if (code in FIXED) {
      const n = FIXED[code];
      if (s.length - i < n) return null;
      ai[code] = s.slice(i, i + n);
      i += n;
    } else if (/^31\d\d$/.test(code)) {
      if (s.length - i < 6) return null;
      ai[code] = s.slice(i, i + 6);
      i += 6;
    } else {
      const max = VAR_MAX[code] ?? 90;
      let end = s.indexOf(GS, i);
      if (end < 0 || end - i > max) end = Math.min(s.length, i + max);
      ai[code] = s.slice(i, end);
      i = end;
    }
  }
  return Object.keys(ai).length ? finish(ai) : null;
}

function finish(ai: Record<string, string>): Gs1 | null {
  const r: Gs1 = { ai };
  if (ai["01"]) r.gtin = ai["01"];
  if (ai["10"]) r.batch = ai["10"];
  if (ai["21"]) r.serial = ai["21"];
  if (ai["11"]) r.producedOn = parseGs1Date(ai["11"]);
  if (ai["17"]) r.expiresOn = parseGs1Date(ai["17"]);
  const w = Object.keys(ai).find((k) => /^310\d$/.test(k));
  if (w) r.weightKg = Number(ai[w]) / 10 ** Number(w[3]);
  if (!r.gtin && !r.expiresOn && r.weightKg === undefined) return null;
  return r;
}

/** Готовая строка для кнопки «Пример: молоко»: GTIN + партия + срок 2026-10-20 + серийный + ключ проверки. */
export const SAMPLE_MILK = `0104607001234567` + `17261020` + `10L2410${GS}21Ab3dE9x${GS}91EE10${GS}92dGVzdHNpZw==`;
