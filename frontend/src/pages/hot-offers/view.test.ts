import { describe, expect, it } from "vitest";
import type { HotOffer } from "../../api/types";
import { filterByProduct, formatUntil, handNote, portionsWord } from "./view";

const offer = (id: string, products: string[]): HotOffer => ({
  dish_id: id, dish_name: id, priority: 1, target_portions: 0, target_today: 0, usual_portions: 0, sell_until: "", explanation: "",
  reasons: products.map((p) => ({ product_id: p, product_name: p, batch_id: 1, at_risk: { qty: 1, unit: "g" }, expires_at: "", working_hours_left: 1 })),
});

describe("formatUntil", () => {
  it("берёт время из строки, а не из пояса браузера", () => {
    expect(formatUntil("2026-10-12T18:30:00+03:00")).toBe("до 18:30 · 12.10");
    expect(formatUntil("")).toBe("");
  });
});

describe("filterByProduct", () => {
  const list = [offer("soup", ["milk"]), offer("salad", ["herbs", "milk"]), offer("steak", ["beef"])];
  it("оставляет блюда, спасающие продукт", () => {
    expect(filterByProduct(list, "milk").map((o) => o.dish_id)).toEqual(["soup", "salad"]);
    expect(filterByProduct(list, "none")).toEqual([]);
  });
  it("без продукта — весь список", () => {
    expect(filterByProduct(list, null)).toBe(list);
  });
});

describe("portionsWord", () => {
  it("склоняет", () => {
    expect([1, 2, 5, 11, 21, 12].map(portionsWord)).toEqual(["порция", "порции", "порций", "порций", "порция", "порций"]);
  });
});

describe("handNote", () => {
  it("суммирует риск по причинам в единице первой", () => {
    const o = offer("d", ["a", "b"]);
    o.reasons[0].at_risk = { qty: 1000, unit: "g" };
    o.reasons[1].at_risk = { qty: 400, unit: "g" };
    expect(handNote(o)).toBe("−1,4 кг в мусор");
  });
  it("без причин пометки нет", () => {
    expect(handNote(offer("d", []))).toBeUndefined();
  });
});
