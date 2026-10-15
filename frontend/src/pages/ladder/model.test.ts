import { describe, expect, it } from "vitest";
import type { LadderBatch, LadderStep } from "../../api/types";
import { ladderSummary, orderedSteps, stepLink, stepValue } from "./model";

const step = (kind: string, over: Partial<LadderStep> = {}): LadderStep => ({
  kind, title: kind, saved_minor: 0, cost_minor: 0, net_minor: 0, detail: "", ...over,
});

const batch = (over: Partial<LadderBatch> = {}): LadderBatch => ({
  batch_id: 17, product_id: "salmon", product_name: "Лосось", at_risk: { qty: 600, unit: "g" }, at_risk_minor: 54000,
  expires_at: "2026-10-16T16:00:00+03:00", best: "sell", combined_minor: 48000,
  steps: [step("transfer", { status: "network" }), step("sell", { saved_minor: 48000, net_minor: 48000 }), step("prep", { status: "roadmap" }), step("discount")],
  ...over,
});

describe("лестница спасения", () => {
  it("ступени идут по порядку лестницы, незнакомые — в конец", () => {
    const b = batch({ steps: [step("future"), ...batch().steps] });
    expect(orderedSteps(b).map((s) => s.kind)).toEqual(["sell", "discount", "prep", "transfer", "future"]);
  });
  it("значение ступени: деньги или честная причина", () => {
    expect(stepValue(step("sell", { net_minor: 48000 }))).toContain("+480");
    expect(stepValue(step("discount", { net_minor: -2000 }))).toContain("−20");
    expect(stepValue(step("prep", { status: "roadmap" }))).toBe("в разработке");
    expect(stepValue(step("transfer", { status: "network" }))).toBe("считает «Сеть»");
    expect(stepValue(step("sell", { status: "unavailable" }))).toBe("нет варианта");
    expect(stepValue(step("sell", { status: "no_price" }))).toBe("нет цены");
  });
  it("итог по партии", () => {
    expect(ladderSummary(batch())).toContain("не спишем ≈");
    expect(ladderSummary(batch({ combined_minor: 0 }))).toContain("не дают выигрыша");
  });
  it("переходы по ступеням", () => {
    expect(stepLink("sell", "salmon")?.href).toBe("#/hot-offers?product_id=salmon");
    expect(stepLink("discount", "salmon")?.href).toBe("#/rescue");
    expect(stepLink("transfer", "salmon")?.href).toBe("#/network");
    expect(stepLink("prep", "salmon")).toBeNull();
  });
});
