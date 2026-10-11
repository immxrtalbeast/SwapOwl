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
};

describe("supply accept", () => {
  it("тело: количество и единица из строки, срок не передаётся", () => {
    const b = buildAcceptBody(row, "12345678-aaaa", "2026-01-01T00:00:00Z");
    expect(b.items).toEqual([{ product_id: "p1", quantity: "1500", unit: "g" }]);
    expect(b.id).toBe("12345678-aaaa");
  });
  it("срок вводится вручную", () => {
    expect(buildAcceptBody(row, "x", "t", 72).items[0].shelf_life_hours).toBe(72);
  });
  it("ключ меняется с количеством", () => {
    expect(offerKey(row)).not.toBe(offerKey({ ...row, quantity: { qty: 2000, unit: "g" } }));
  });
  it("replayed → «Поставка уже принята»", () => {
    expect(outcomeFromResult(true).message).toBe("Поставка уже принята");
    expect(outcomeFromResult(false).kind).toBe("accepted");
  });
  it("400 про срок хранения → поле ввода срока", () => {
    const e = new ApiError(400, "validation_error", 'product "p1" has no default shelf life: pass shelf_life_hours or expires_on', {});
    expect(outcomeFromError(e).kind).toBe("need-shelf-life");
    expect(outcomeFromError(new ApiError(500, "internal", "boom")).kind).toBe("error");
  });
});
