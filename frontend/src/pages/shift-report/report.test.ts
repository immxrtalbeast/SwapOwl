import { describe, expect, it } from "vitest";
import type { Order } from "../../api/types";
import { dayOf, dishLines, sumMinor, sumPortions } from "./report";

const order = (at: string, items: { dish_id: string; portions: number }[]): Order => ({
  id: at, status: "accepted", at, replayed: false, items, writeoffs: [], missing: [],
});

describe("dayOf", () => {
  it("считает сутки по поясу стенда", () => {
    expect(dayOf("2026-10-12T22:30:00Z", "Europe/Moscow")).toBe("2026-10-13");
    expect(dayOf("2026-10-12T22:30:00Z", "UTC")).toBe("2026-10-12");
  });
});

describe("dishLines", () => {
  const orders = [
    order("2026-10-13T08:00:00Z", [{ dish_id: "a", portions: 2 }]),
    order("2026-10-13T09:00:00Z", [{ dish_id: "a", portions: 1 }, { dish_id: "b", portions: 4 }]),
    order("2026-10-12T09:00:00Z", [{ dish_id: "a", portions: 9 }]),
  ];
  it("суммирует порции за выбранные сутки и умножает на цену", () => {
    const lines = dishLines(orders, [{ id: "a", name: "Борщ", price_minor: 30000 }, { id: "b", name: "Суп" }], "2026-10-13", "UTC");
    expect(lines).toEqual([
      { dish_id: "a", name: "Борщ", portions: 3, minor: 90000 },
      { dish_id: "b", name: "Суп", portions: 4, minor: null },
    ]);
    expect(sumMinor(lines)).toBe(90000);
    expect(sumPortions(lines)).toBe(7);
  });
  it("без цен сумма в рублях неизвестна, а не ноль", () => {
    const lines = dishLines(orders, [{ id: "a", name: "Борщ" }], "2026-10-13", "UTC");
    expect(sumMinor(lines)).toBeNull();
  });
});
