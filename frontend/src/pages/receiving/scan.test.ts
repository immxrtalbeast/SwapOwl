import { describe, expect, it } from "vitest";
import type { ProductStock } from "../../api/types";
import { parseGs1 } from "../../lib/gs1";
import { lineFromScan, toDeliveryItem } from "./scan";

const p = { id: "milk", name: "Молоко", unit: "ml" } as ProductStock;

describe("toDeliveryItem", () => {
  it("вес уходит в kg", () => {
    const l = lineFromScan(parseGs1("0104607001234567310300125017261020")!, p, 1);
    expect(toDeliveryItem(l, [p])).toMatchObject({ quantity: "1.25", unit: "kg" });
  });
  it("без веса — pack", () => {
    const l = lineFromScan(parseGs1("01046070012345671726102010A")!, p, 1);
    expect(toDeliveryItem(l, [p])).toMatchObject({ quantity: "1", unit: "pack", expires_on: "2026-10-20" });
  });
});
