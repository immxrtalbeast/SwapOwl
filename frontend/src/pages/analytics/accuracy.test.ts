import { describe, expect, it } from "vitest";
import type { ForecastAccuracy } from "../../api/types";
import { accuracyDays, accuracySentence, worstDishes } from "./accuracy";

const base: ForecastAccuracy = {
  days: 7, days_used: 6, enough_data: true, wape_pct: 23.4, baseline_wape_pct: 41,
  dishes: [
    { dish_id: "a", dish_name: "Суп", actual_per_day: 10, predicted_per_day: 8, wape_pct: 30, baseline_wape_pct: 50 },
    { dish_id: "b", dish_name: "Редкое", actual_per_day: 0, predicted_per_day: 1, wape_pct: 0, baseline_wape_pct: 0 },
  ],
};

describe("точность прогноза", () => {
  it("период проверки: не меньше недели и не больше 30 суток", () => {
    expect(accuracyDays(1)).toBe(7);
    expect(accuracyDays(7)).toBe(7);
    expect(accuracyDays(30)).toBe(30);
    expect(accuracyDays(90)).toBe(30);
  });
  it("фраза: ошибка с обучением и против априора", () => {
    expect(accuracySentence(base)).toBe(
      "Прогноз спроса за 6 суток ошибался в среднем на 23,4 %. Без обучения, по одному априору, ошибка была бы 41 % — движок точнее на 17,6 %.",
    );
  });
  it("обучение не помогло — говорим как есть", () => {
    expect(accuracySentence({ ...base, wape_pct: 41, baseline_wape_pct: 40 })).toContain("обучение пока не добавило точности");
  });
  it("мало истории — честное «пока рано»", () => {
    expect(accuracySentence({ ...base, enough_data: false, days_used: 1 })).toBe(
      "Пока мало истории: проверено 1 из 7 суток, нужно хотя бы 3 с заказами.",
    );
  });
  it("в таблицу — только блюда с фактом", () => {
    expect(worstDishes(base).map((d) => d.dish_id)).toEqual(["a"]);
  });
});
