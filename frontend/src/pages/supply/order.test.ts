import { describe, expect, it } from "vitest";
import type { SupplySuggestion } from "../../api/types";
import { ORDER_STATUS_TEXT, buildOrderBody, buildOrderCsv, needsPolling, selectionKey } from "./order";

const row = (id: string, qty: number): SupplySuggestion => ({
  product_id: id,
  product_name: id,
  quantity: { qty, unit: "g" },
  usable_stock: { qty: 0, unit: "g" },
  rate_per_day: { qty: 1, unit: "g" },
  stockout_at: null,
  blocks_dishes: 0,
  urgency: "now",
  explanation: "",
});

describe("supply order", () => {
  it("тело: id и позиции в единицах продукта", () => {
    expect(buildOrderBody([row("a", 1500), row("b", 20)], "id-1")).toEqual({
      id: "id-1",
      items: [
        { product_id: "a", qty: 1500 },
        { product_id: "b", qty: 20 },
      ],
    });
  });
  it("ключ выбора не зависит от порядка и меняется с количеством", () => {
    expect(selectionKey([row("a", 1), row("b", 2)])).toBe(selectionKey([row("b", 2), row("a", 1)]));
    expect(selectionKey([row("a", 1)])).not.toBe(selectionKey([row("a", 2)]));
  });
  it("статусы на экране", () => {
    expect(ORDER_STATUS_TEXT).toEqual({ sent: "отправлен", delivered: "доставлен поставщику", error: "ошибка доставки" });
    expect(needsPolling("sent")).toBe(true);
    expect(needsPolling("error")).toBe(true);
    expect(needsPolling("delivered")).toBe(false);
  });
  it("CSV: заголовок, строки, экранирование", () => {
    expect(buildOrderCsv([row("a;b", 1500)])).toBe('\uFEFFПродукт;Количество;Единица\r\n"a;b";1500;g\r\n');
  });
});
