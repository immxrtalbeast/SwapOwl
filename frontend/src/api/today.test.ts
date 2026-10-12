import { describe, expect, it } from "vitest";
import { atRiskFromAlerts, restaurantDay, todayMinor, qtyCostMinor, unitPricesFromWriteoffs } from "./today";
import type { Alert, MoneyReport } from "./types";

const report = {
  from: "", to: "", total_minor: 0, by_day: [],
  by_product: [
    { product_id: "milk", product_name: "Молоко", quantity: { qty: 1000, unit: "ml" }, cost_minor: 8000 },
    { product_id: "milk", product_name: "Молоко", quantity: { qty: 1000, unit: "ml" }, cost_minor: 8000 },
    { product_id: "zero", product_name: "Х", quantity: { qty: 5, unit: "pcs" }, cost_minor: 0 },
  ],
} as MoneyReport;

const alert = (id: string, status: Alert["status"], qty: number): Alert => ({
  batch_id: 1, product_id: id, product_name: id, remaining: { qty, unit: "ml" }, expires_at: "", time_left_seconds: 1, status, doomed: false,
});

describe("today model", () => {
  it("выводит цену единицы из списаний", () => {
    const p = unitPricesFromWriteoffs(report);
    expect(p).toEqual({ milk: 8 });
    expect(qtyCostMinor("milk", { qty: 500, unit: "ml" }, p)).toBe(4000);
    expect(qtyCostMinor("nope", { qty: 1, unit: "g" }, p)).toBeNull();
  });
  it("считает только истекающие, без цены — отдельно", () => {
    const r = atRiskFromAlerts([alert("milk", "expiring", 500), alert("milk", "expired", 900), alert("nope", "expiring", 1)], { milk: 8 });
    expect(r).toEqual({ totalMinor: 4000, unpriced: 1 });
  });
  it("«сегодня» — по дню ресторана, а не по скользящим 24 ч", () => {
    const r = { ...report, by_day: [{ date: "2026-10-11", cost_minor: 500 }, { date: "2026-10-12", cost_minor: 700 }] } as MoneyReport;
    // 21:30 UTC 11 октября = 00:30 12 октября в Москве
    expect(restaurantDay("2026-10-11T21:30:00Z", "Europe/Moscow")).toBe("2026-10-12");
    expect(todayMinor(r, "2026-10-11T21:30:00Z", "Europe/Moscow")).toBe(700);
    expect(todayMinor(r, "2026-10-11T21:30:00Z", "UTC")).toBe(500);
    expect(todayMinor(r, "2026-10-13T09:00:00Z", "UTC")).toBe(0);
  });
});
