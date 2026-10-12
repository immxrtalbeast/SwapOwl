import { describe, expect, it } from "vitest";
import type { HotOffer } from "../../api/types";
import { actionSavedMinor, realisticTarget } from "./realistic";

const offer = (target: number, usual: number): HotOffer => ({
  dish_id: "d", dish_name: "Стейк из лосося", priority: 1, target_portions: target, target_today: target, usual_portions: usual, sell_until: "", explanation: "",
  reasons: [{ product_id: "salmon", product_name: "лосось", batch_id: 1, at_risk: { qty: 1600, unit: "g" }, expires_at: "", working_hours_left: 4 }],
});

describe("realisticTarget", () => {
  it("цель 8 при обычных 1 → реально 2, остаток с действиями", () => {
    const r = realisticTarget(offer(8, 1))!;
    expect(r.real).toBe(2);
    expect(r.leftoverPortions).toBe(6);
    expect(r.leftover).toEqual({ name: "лосось", productId: "salmon", qty: { qty: 1200, unit: "g" } });
    expect(actionSavedMinor(r, "staff", { salmon: 2 })).toBe(2400);
    expect(actionSavedMinor(r, "discount", { salmon: 2 })).toBe(1920);
  });
  it("обычный темп 0 → реально 1", () => {
    expect(realisticTarget(offer(5, 0))!.real).toBe(1);
  });
  it("цель в пределах 2× — без изменений", () => {
    expect(realisticTarget(offer(2, 1))).toBeNull();
    expect(realisticTarget(offer(6, 3))).toBeNull();
  });
  it("без цены продукта суммы нет", () => {
    expect(actionSavedMinor(realisticTarget(offer(8, 1))!, "prep", {})).toBeNull();
  });
});
