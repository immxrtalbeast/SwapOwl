import { describe, expect, it } from "vitest";
import type { DishServings, HotOffersResponse } from "../../api/types";
import { hallCards, pickableDishes } from "./manager";

const menu = [
  { id: "soup", name: "Суп", available: true },
  { id: "steak", name: "Стейк", available: true },
  { id: "burger", name: "Бургер", available: false },
  { id: "pasta", name: "Паста", available: true },
  { id: "salad", name: "Салат", available: true },
] as unknown as DishServings[];

const soon = new Date(Date.now() + 3 * 3_600_000).toISOString();
const data: HotOffersResponse = {
  algo: "b",
  offers: [
    { dish_id: "steak", dish_name: "Стейк", priority: 1, target_portions: 5, target_today: 5, usual_portions: 2, sell_until: soon, explanation: "",
      reasons: [{ product_id: "salmon", product_name: "Лосось", batch_id: 1, at_risk: { qty: 400, unit: "g" }, expires_at: soon, working_hours_left: 3 }] },
    { dish_id: "pasta", dish_name: "Паста", priority: 0.5, target_portions: 2, target_today: 2, usual_portions: 1, sell_until: soon, explanation: "", reasons: [], pinned: true, manager_note: "шеф советует" },
  ],
  excluded: [],
  manager_picks: [{ dish_id: "soup", dish_name: "Суп", note: "к обеду", set_at: "", set_by: "" }],
  hidden: [{ dish_id: "salad", dish_name: "Салат", in_offers: true, set_at: "", set_by: "" }],
} as unknown as HotOffersResponse;

describe("ручная правка горячих предложений", () => {
  it("добавить можно только доступное и ещё не участвующее блюдо", () => {
    expect(pickableDishes(menu, data).map((d) => d.id)).toEqual([]);
    expect(pickableDishes(menu, { ...data, manager_picks: [], hidden: [] }).map((d) => d.id)).toEqual(["salad", "soup"]);
  });

  it("для зала: расчёт с короткой причиной, закреплённые и добавленные — от менеджера", () => {
    expect(hallCards(data)).toEqual([
      { dishId: "steak", dish: "Стейк", why: "лосось нужно продать сегодня", byManager: false },
      { dishId: "pasta", dish: "Паста", why: "шеф советует", byManager: true },
      { dishId: "soup", dish: "Суп", why: "к обеду", byManager: true },
    ]);
  });
});
