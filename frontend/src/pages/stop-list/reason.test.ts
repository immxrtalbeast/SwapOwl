import { describe, expect, it } from "vitest";
import type { DishServings } from "../../api/types";
import { reasonText, servingsLabel } from "./reason";

const stopped: DishServings = {
  id: "steak",
  name: "Стейк",
  available: false,
  servings: 0,
  recipe: [],
  missing: [
    {
      product_id: "beef",
      product_name: "Говядина",
      required: { qty: 200, unit: "g" },
      available: { qty: 0, unit: "g" },
      missing: { qty: 200, unit: "g" },
    },
    {
      product_id: "veg",
      product_name: "Овощи",
      required: { qty: 50, unit: "g" },
      available: { qty: 20, unit: "g" },
      missing: { qty: 30, unit: "g" },
    },
  ],
};

describe("stop-list text", () => {
  it("names the product and how much is missing", () => {
    expect(reasonText(stopped)).toBe("Говядина: закончился или просрочен; Овощи: не хватает 30 г на порцию");
  });
  it("labels servings", () => {
    expect(servingsLabel(stopped)).toBe("нет в наличии");
    expect(servingsLabel({ ...stopped, available: true, servings: 4, missing: [] })).toBe("4 порц.");
  });
});
