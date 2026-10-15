import { describe, expect, it } from "vitest";
import type { Alert, DishServings } from "../../api/types";
import { batchesWord, isTgHash, stoppedDishes, tgHash, tgTabFromHash, themeToVars, todaySummary } from "./model";

const alert = (over: Partial<Alert>): Alert => ({
  batch_id: 1,
  product_id: "milk",
  product_name: "Молоко",
  remaining: { qty: 1000, unit: "ml" },
  expires_at: "2026-10-16T00:00:00Z",
  time_left_seconds: 3600,
  status: "expiring",
  doomed: false,
  ...over,
});

const dish = (id: string, name: string, available: boolean): DishServings => ({ id, name, available, servings: available ? 3 : 0, missing: [], recipe: [] });

describe("маршрут мини-приложения", () => {
  it("узнаёт #/tg и вложенные вкладки, но не чужие экраны", () => {
    expect(isTgHash("#/tg")).toBe(true);
    expect(isTgHash("#tg/manager")).toBe(true);
    expect(isTgHash("#/tg?x=1")).toBe(true);
    expect(isTgHash("#/today")).toBe(false);
    expect(isTgHash("#/tgx")).toBe(false);
    expect(isTgHash("")).toBe(false);
  });
  it("вкладка по хэшу: по умолчанию «Зал»", () => {
    expect(tgTabFromHash("#/tg")).toBe("hall");
    expect(tgTabFromHash("#/tg/receiving")).toBe("receiving");
    expect(tgTabFromHash("#/tg/manager?x=1")).toBe("manager");
    expect(tgTabFromHash("#/tg/nope")).toBe("hall");
  });
  it("хэш вкладки обратим", () => {
    for (const t of ["hall", "receiving", "manager"] as const) expect(tgTabFromHash(tgHash(t))).toBe(t);
  });
});

describe("themeToVars", () => {
  it("переносит цвета Telegram на наши токены", () => {
    const v = themeToVars({ bg_color: "#111111", text_color: "#ffffff", button_color: "#2481cc" }) as Record<string, string>;
    expect(v["--surface"]).toBe("#111111");
    expect(v["--text"]).toBe("#ffffff");
    expect(v["--primary"]).toBe("#2481cc");
  });
  it("без темы и с некорректными значениями ничего не меняет", () => {
    expect(themeToVars(undefined)).toEqual({});
    expect(themeToVars({ bg_color: "url(javascript:1)", text_color: "red" })).toEqual({});
  });
});

describe("todaySummary", () => {
  it("считает под риском только «истекает» с ценой, партии и стоп", () => {
    const alerts = [alert({}), alert({ batch_id: 2, product_id: "x" }), alert({ batch_id: 3, status: "expired" })];
    const s = todaySummary(alerts, { milk: 0.5 }, [dish("a", "А", true), dish("b", "Б", false)]);
    expect(s).toEqual({ atRiskMinor: 500, unpriced: 1, expiring: 2, stopped: 1 });
  });
});

describe("стоп-лист и слова", () => {
  it("только недоступные, по алфавиту", () => {
    expect(stoppedDishes([dish("1", "Суп", false), dish("2", "Борщ", true), dish("3", "Пицца", false)]).map((d) => d.name)).toEqual(["Пицца", "Суп"]);
  });
  it("склоняет «партия»", () => {
    expect([1, 2, 5, 11, 21, 12].map(batchesWord)).toEqual(["1 партия", "2 партии", "5 партий", "11 партий", "21 партия", "12 партий"]);
  });
});
