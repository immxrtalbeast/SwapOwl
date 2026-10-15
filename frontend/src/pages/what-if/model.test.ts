import { describe, expect, it } from "vitest";
import type { Dish, ProductStock } from "../../api/types";
import { buildRequest, changedProducts, newDraft, parseValue, scenarioTitle, verdict } from "./model";

const dishes: Dish[] = [{ id: "caesar", name: "Цезарь", recipe: [] }];
const products = [{ id: "chicken", name: "Курица" }] as unknown as ProductStock[];

describe("parseValue", () => {
  it("принимает запятую и точку, мусор — NaN", () => {
    expect(parseValue("1,5")).toBe(1.5);
    expect(parseValue(" 20 ")).toBe(20);
    expect(parseValue("")).toBeNaN();
    expect(parseValue("1e3")).toBeNaN();
    expect(parseValue("-5")).toBeNaN();
  });
});

describe("buildRequest", () => {
  it("собирает тело по типам сценариев", () => {
    const d = [
      { key: 1, type: "discount" as const, targetId: "caesar", value: "20" },
      { key: 2, type: "order_less" as const, targetId: "chicken", value: "30" },
      { key: 3, type: "demand" as const, targetId: "caesar", value: "0,5" },
    ];
    const { request, errors } = buildRequest(3, d);
    expect(errors).toEqual({});
    expect(request).toEqual({
      horizon_days: 3,
      scenarios: [
        { type: "discount", dish_id: "caesar", percent: 20 },
        { type: "order_less", product_id: "chicken", percent: 30 },
        { type: "demand", dish_id: "caesar", factor: 0.5 },
      ],
    });
  });
  it("значение вне границ — ошибка в строке, запроса нет", () => {
    const { request, errors } = buildRequest(3, [{ key: 7, type: "discount", targetId: "caesar", value: "80" }]);
    expect(request).toBeNull();
    expect(errors[7]).toMatch(/от 5 до 50/);
  });
  it("без сценариев — просьба добавить", () => {
    const { request, errors } = buildRequest(3, []);
    expect(request).toBeNull();
    expect(errors[-1]).toBeTruthy();
  });
  it("не выбрано блюдо — ошибка", () => {
    expect(buildRequest(1, [{ key: 1, type: "demand", targetId: "", value: "1" }]).errors[1]).toBe("Выберите блюдо");
  });
});

describe("newDraft", () => {
  it("берёт первое блюдо или продукт и стартовое значение типа", () => {
    expect(newDraft(1, "discount", dishes, products)).toEqual({ key: 1, type: "discount", targetId: "caesar", value: "20" });
    expect(newDraft(2, "order_less", dishes, products)).toMatchObject({ targetId: "chicken", value: "30" });
  });
});

describe("scenarioTitle и verdict", () => {
  it("называет сценарии словами", () => {
    expect(scenarioTitle({ type: "discount", dish_id: "caesar", percent: 20 }, dishes, products)).toBe("Скидка 20 % на «Цезарь»");
    expect(scenarioTitle({ type: "demand", dish_id: "caesar", factor: 0.5 }, dishes, products)).toBe("Спрос на «Цезарь» ×0,5");
    expect(scenarioTitle({ type: "order_less", product_id: "chicken", percent: 30 }, dishes, products)).toBe("Заказать «Курица» на 30 % меньше");
  });
  it("меньше списаний — экономия, больше — потери", () => {
    expect(verdict({ delta_minor: -120000, delta_pct: -18.26 })).toMatchObject({ tone: "good" });
    expect(verdict({ delta_minor: -120000, delta_pct: -18.26 }).headline).toContain("1");
    expect(verdict({ delta_minor: 5000, delta_pct: 4 }).tone).toBe("bad");
    expect(verdict({ delta_minor: 0, delta_pct: 0 }).tone).toBe("same");
  });
  it("в таблице только изменившиеся продукты, крупные сверху", () => {
    const rows = changedProducts([{ delta_minor: 0 }, { delta_minor: -100 }, { delta_minor: 5000 }]);
    expect(rows.map((r) => r.delta_minor)).toEqual([5000, -100]);
  });
});
