import { describe, expect, it } from "vitest";
import { benchSentence } from "./benchText";

describe("benchSentence", () => {
  it("при available: true — обе цифры и число прогонов", () => {
    expect(
      benchSentence({ available: true, runs: 200, main_algo: "b", waste_reduction_vs_random_pct: 10.1, waste_reduction_vs_nearest_pct: 13.3 }),
    ).toBe("Горячие предложения снижают списания на 10,1 % против случайных рекомендаций и на 13,3 % против подсказки «блюдо с ближайшим сроком». Симулятор, 200 прогонов.");
  });
  it("без результата цифры не выдаются", () => {
    expect(benchSentence({ available: false })).toBeNull();
    expect(benchSentence(null)).toBeNull();
    expect(benchSentence({ available: true, runs: 200 })).toBeNull();
  });
});
