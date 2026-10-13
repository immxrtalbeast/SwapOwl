import { describe, expect, it } from "vitest";
import type { Order } from "../../api/types";
import { appendPage, itemsText, rejectionReason, sectionTitle, shortfallText, writeoffLines } from "./view";

const w = (batch: number, qty: number, name = "Лосось") => ({
  product_id: "salmon",
  product_name: name,
  batch_id: batch,
  qty: { qty, unit: "g" as const },
  expires_at: "2026-10-12T09:00:00+03:00",
});

describe("история заказов", () => {
  it("списание по партиям: продукт, количество, номер партии", () => {
    expect(writeoffLines({ writeoffs: [w(17, 500)] })).toEqual(["Лосось 500 г — партия №17"]);
    expect(writeoffLines({ writeoffs: [w(17, 1500)] })).toEqual(["Лосось 1,5 кг — партия №17"]);
  });
  it("две партии — две строки; повтор той же партии складывается", () => {
    expect(writeoffLines({ writeoffs: [w(17, 300), w(18, 200), w(17, 200)] })).toEqual([
      "Лосось 500 г — партия №17",
      "Лосось 200 г — партия №18",
    ]);
  });
  it("без названия продукта (старый ответ) — id", () => {
    expect(writeoffLines({ writeoffs: [{ ...w(3, 100), product_name: undefined }] })).toEqual(["Salmon 100 г — партия №3"]);
  });
  it("причина отказа человеческим языком", () => {
    const m = (name: string, qty: number, unit: "ml" | "g") => ({
      product_id: name, product_name: name, required: { qty: 0, unit }, available: { qty: 0, unit }, missing: { qty, unit },
    });
    expect(rejectionReason([m("Сливки", 200, "ml")])).toBe("Не хватает: сливки 200 мл на заказ");
    expect(rejectionReason([m("Сливки", 200, "ml"), m("Лосось", 1200, "g")])).toBe("Не хватает: сливки 200 мл, лосось 1,2 кг на заказ");
    expect(rejectionReason([])).toBe("Не хватает продуктов на заказ");
  });
  it("недостача кассы", () => {
    expect(shortfallText(undefined)).toBeNull();
    expect(shortfallText([])).toBeNull();
    expect(shortfallText([{ product_id: "c", product_name: "Сливки", quantity: { qty: 50, unit: "ml" } }])).toBe("Не хватило на складе: сливки 50 мл");
  });
  it("заголовок — total, а не размер страницы", () => {
    expect(sectionTitle("Принятые", 1284)).toBe("Принятые · 1 284");
    expect(sectionTitle("Отклонённые", 0)).toBe("Отклонённые · 0");
  });
  it("блюда и подгрузка без дублей", () => {
    const o = (id: string): Order => ({ id, status: "accepted", at: "", replayed: false, items: [{ dish_id: "d", portions: 2 }], writeoffs: [], missing: [] });
    expect(itemsText(o("a"), () => "Суп")).toBe("Суп × 2");
    expect(appendPage([o("a"), o("b")], [o("b"), o("c")]).map((x) => x.id)).toEqual(["a", "b", "c"]);
  });
});
