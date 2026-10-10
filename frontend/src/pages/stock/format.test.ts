import { describe, expect, it } from "vitest";
import { formatQty, formatTimeLeft } from "./format";

describe("format", () => {
  it("formats quantities in human units", () => {
    expect(formatQty({ qty: 1200, unit: "g" })).toBe("1,2 кг");
    expect(formatQty({ qty: 600, unit: "ml" })).toBe("600 мл");
    expect(formatQty({ qty: 3, unit: "pcs" })).toBe("3 шт");
  });
  it("formats relative time", () => {
    expect(formatTimeLeft(14 * 3600)).toBe("через 14 ч");
    expect(formatTimeLeft(-3 * 3600)).toBe("просрочено 3 ч назад");
  });
});
