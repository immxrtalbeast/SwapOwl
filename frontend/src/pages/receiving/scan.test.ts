import { describe, expect, it } from "vitest";
import type { ProductStock } from "../../api/types";
import { SAMPLE_MILK, parseGs1 } from "../../lib/gs1";
import { isLineReady, lineFromScan, loadGtinMap, toDeliveryItem } from "./scan";

const p = { id: "milk", name: "Молоко", unit: "ml" } as ProductStock;

describe("toDeliveryItem", () => {
  it("вес уходит в kg", () => {
    const l = lineFromScan(parseGs1("0104607001234567310300125017261020")!, p, 1);
    expect(toDeliveryItem(l, [p])).toMatchObject({ quantity: "1.25", unit: "kg" });
  });
  it("молоко: 1 скан без веса → 1000 мл", () => {
    const l = lineFromScan(parseGs1(SAMPLE_MILK)!, p, 1);
    expect(toDeliveryItem(l, [p])).toMatchObject({ quantity: "1000", unit: "ml", expires_on: "2026-10-20" });
    expect(isLineReady(l)).toBe(true);
  });
  it("объём из AI 315n уходит в l", () => {
    const l = lineFromScan(parseGs1("0104607001234567315100001017261020")!, p, 1);
    expect(toDeliveryItem(l, [p])).toMatchObject({ quantity: "1", unit: "l" });
  });
  it("без упаковки и веса — количество пусто, отправить нельзя", () => {
    const x = { id: "salt", name: "Соль", unit: "g" } as ProductStock;
    const l = lineFromScan(parseGs1("01046070012345671726102010A")!, x, 1);
    expect(l.quantity).toBe("");
    expect(isLineReady(l)).toBe(false);
    expect(isLineReady({ ...l, quantity: "500" })).toBe(true);
  });
  it("пример молока привязан к milk без запоминания", () => {
    expect(loadGtinMap()["04607001234567"]).toBe("milk");
  });
});
