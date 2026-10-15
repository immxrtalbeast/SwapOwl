import type { Dish, ProductStock, WhatIfRequest, WhatIfResult, WhatIfScenarioInput, WhatIfType } from "../../api/types";
import { formatRub } from "../money/format";

/** Не больше, чем принимает API (internal/api/whatif.go). */
export const MAX_SCENARIOS = 5;
export const HORIZONS = [1, 2, 3, 5, 7] as const;

export interface Draft {
  key: number;
  type: WhatIfType;
  targetId: string;
  /** Процент (скидка, «заказать меньше») или коэффициент спроса — как ввёл человек. */
  value: string;
}

interface Spec {
  label: string;
  /** Что выбираем: блюдо или продукт. */
  target: "dish" | "product";
  min: number;
  max: number;
  unit: string;
  start: string;
  hint: string;
}

export const SPECS: Record<WhatIfType, Spec> = {
  discount: { label: "Скидка на блюдо", target: "dish", min: 5, max: 50, unit: "%", start: "20", hint: "от 5 до 50 %" },
  order_less: { label: "Заказать меньше", target: "product", min: 10, max: 100, unit: "%", start: "30", hint: "от 10 до 100 % остатка" },
  demand: { label: "Спрос на блюдо", target: "dish", min: 0.5, max: 2, unit: "×", start: "0,5", hint: "от 0,5 до 2 раз от обычного" },
};

export const TYPE_ORDER: WhatIfType[] = ["discount", "order_less", "demand"];

export function newDraft(key: number, type: WhatIfType, dishes: readonly Dish[], products: readonly ProductStock[]): Draft {
  const spec = SPECS[type];
  const first = spec.target === "dish" ? dishes[0]?.id : products[0]?.id;
  return { key, type, targetId: first ?? "", value: spec.start };
}

/** «1,5» и «1.5» — одно число; пусто и мусор — NaN. */
export function parseValue(s: string): number {
  const t = s.trim().replace(",", ".");
  return t === "" || !/^\d+(\.\d+)?$/.test(t) ? NaN : Number(t);
}

export interface Built {
  request: WhatIfRequest | null;
  /** Ошибки по ключу сценария — показываем в его строке. */
  errors: Record<number, string>;
}

export function buildRequest(horizon: number, drafts: readonly Draft[]): Built {
  const errors: Record<number, string> = {};
  const scenarios: WhatIfScenarioInput[] = [];
  for (const d of drafts) {
    const spec = SPECS[d.type];
    const v = parseValue(d.value);
    if (!d.targetId) {
      errors[d.key] = spec.target === "dish" ? "Выберите блюдо" : "Выберите продукт";
      continue;
    }
    if (!Number.isFinite(v) || v < spec.min || v > spec.max) {
      errors[d.key] = `Значение — ${spec.hint}`;
      continue;
    }
    const s: WhatIfScenarioInput = { type: d.type };
    if (spec.target === "dish") s.dish_id = d.targetId;
    else s.product_id = d.targetId;
    if (d.type === "demand") s.factor = v;
    else s.percent = v;
    scenarios.push(s);
  }
  if (scenarios.length === 0 && Object.keys(errors).length === 0) errors[-1] = "Добавьте хотя бы один сценарий";
  const bad = Object.keys(errors).length > 0;
  return { request: bad ? null : { horizon_days: horizon, scenarios }, errors };
}

const num = (v: number) => String(v).replace(".", ",");

/** «Скидка 20 % на «Цезарь»» — заголовок результата. */
export function scenarioTitle(r: WhatIfScenarioInput, dishes: readonly Dish[], products: readonly ProductStock[]): string {
  const dish = dishes.find((d) => d.id === r.dish_id)?.name ?? r.dish_id ?? "";
  const product = products.find((p) => p.id === r.product_id)?.name ?? r.product_id ?? "";
  switch (r.type) {
    case "discount": return `Скидка ${num(r.percent ?? 0)} % на «${dish}»`;
    case "order_less": return `Заказать «${product}» на ${num(r.percent ?? 0)} % меньше`;
    case "demand": return `Спрос на «${dish}» ×${num(r.factor ?? 1)}`;
  }
}

export interface Verdict {
  tone: "good" | "bad" | "same";
  headline: string;
  detail: string;
}

/** Меньше списаний — хорошо. Сумма в рублях и процент словами. */
export function verdict(r: Pick<WhatIfResult, "delta_minor" | "delta_pct">): Verdict {
  const pct = `${num(Math.round(Math.abs(r.delta_pct) * 10) / 10)} %`;
  if (r.delta_minor < 0) return { tone: "good", headline: `Сэкономим ${formatRub(-r.delta_minor)}`, detail: `списаний меньше на ${pct}` };
  if (r.delta_minor > 0) return { tone: "bad", headline: `Потеряем на ${formatRub(r.delta_minor)} больше`, detail: `списаний больше на ${pct}` };
  return { tone: "same", headline: "Списания не изменятся", detail: "сценарий не меняет прогноз" };
}

/** Продукты, в которых сценарий что-то меняет, — самые заметные сверху. */
export function changedProducts<T extends { delta_minor: number }>(rows: readonly T[]): T[] {
  return rows.filter((r) => r.delta_minor !== 0).sort((a, b) => Math.abs(b.delta_minor) - Math.abs(a.delta_minor));
}
