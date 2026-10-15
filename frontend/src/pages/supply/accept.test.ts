import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/types";
import type { SupplySuggestion } from "../../api/types";
import { buildAcceptBody, offerKey, outcomeFromError, outcomeFromResult } from "./accept";

const row: SupplySuggestion = {
  product_id: "p1",
  product_name: "Мука",
  quantity: { qty: 1500, unit: "g" },
  usable_stock: { qty: 0, unit: "g" },
  rate_per_day: { qty: 100, unit: "g" },
  stockout_at: null,
  blocks_dishes: 0,
  urgency: "now",
  explanation: "",
  shelf_life_hours: null,
};

describe("supply accept", () => {
  it("тело: количество, единица и срок поставщика из строки — срок всегда явно (FS-160)", () => {
    const b = buildAcceptBody({ ...row, shelf_life_hours: 96 }, "12345678-aaaa", "2026-01-01T00:00:00Z");
    expect(b.items).toEqual([{ product_id: "p1", quantity: "1500", unit: "g", shelf_life_hours: 96 }]);
    expect(b.id).toBe("12345678-aaaa");
  });
  it("срок, введённый вручную, главнее; без срока поставщика поле срока не подставляется", () => {
    expect(buildAcceptBody({ ...row, shelf_life_hours: 96 }, "x", "t", 72).items[0].shelf_life_hours).toBe(72);
    expect(buildAcceptBody(row, "x", "t").items[0]).not.toHaveProperty("shelf_life_hours");
  });
  it("ключ меняется с количеством", () => {
    expect(offerKey(row)).not.toBe(offerKey({ ...row, quantity: { qty: 2000, unit: "g" } }));
  });
  it("replayed → «Поставка уже принята»", () => {
    expect(outcomeFromResult(true).message).toBe("Поставка уже принята");
    expect(outcomeFromResult(false).kind).toBe("accepted");
  });
  it("400 про срок хранения → поле ввода срока", () => {
    const e = new ApiError(400, "validation_error", "срок годности обязателен в каждой позиции поставки", { field: "items[0].expires_on" });
    expect(outcomeFromError(e).kind).toBe("need-shelf-life");
    expect(outcomeFromError(new ApiError(500, "internal", "boom")).kind).toBe("error");
  });
});
