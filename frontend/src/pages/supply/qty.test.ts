import { describe, expect, it } from "vitest";
import { NBSP as N } from "../../lib/format";
import { formatSupplyQty } from "./qty";

describe("formatSupplyQty", () => {
  it("кг и л — два знака после запятой", () => {
    expect(formatSupplyQty({ qty: 4238, unit: "g" })).toBe(`4,24${N}кг`);
    expect(formatSupplyQty({ qty: 10465, unit: "g" })).toBe(`10,47${N}кг`);
    expect(formatSupplyQty({ qty: 1500, unit: "ml" })).toBe(`1,5${N}л`);
  });
  it("малые количества без изменений", () => {
    expect(formatSupplyQty({ qty: 350, unit: "g" })).toBe(`350${N}г`);
    expect(formatSupplyQty({ qty: 0, unit: "ml" })).toBe(`0${N}мл`);
  });
});
