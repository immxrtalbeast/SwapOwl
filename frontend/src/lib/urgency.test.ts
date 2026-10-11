import { describe, expect, it } from "vitest";
import { batchState, dishState, toneVars, worstBatchState, BATCH_STATES } from "./urgency";

describe("batchState", () => {
  it("порядок проверок spent → expired → doomed → expiring → ok", () => {
    expect(batchState({ remaining: 0, status: "expired" })).toBe("spent");
    expect(batchState({ remaining: 5, status: "expired", doomed: true })).toBe("expired");
    expect(batchState({ remaining: 5, status: "expiring", doomed: true })).toBe("doomed");
    expect(batchState({ remaining: 5, status: "expiring", doomed: false })).toBe("expiring");
    expect(batchState({ remaining: 5, status: "ok" })).toBe("ok");
  });
  it("тона шкалы", () => {
    expect(BATCH_STATES.doomed.tone).toBe("risk");
    expect(BATCH_STATES.expired.tone).toBe("critical");
  });
});

describe("worstBatchState", () => {
  it("ранг doomed > expiring > expired > ok > spent", () => {
    expect(worstBatchState(["ok", "spent", "expired"])).toBe("expired");
    expect(worstBatchState(["expired", "expiring", "doomed"])).toBe("doomed");
    expect(worstBatchState(["expired", "expiring"])).toBe("expiring");
    expect(worstBatchState(["spent", "ok"])).toBe("ok");
    expect(worstBatchState([])).toBe("spent");
  });
});

describe("dishState", () => {
  it("порог 10 порций только визуальный", () => {
    expect(dishState({ available: false, portions: 0 })).toBe("stopped");
    expect(dishState({ available: true, portions: 9 })).toBe("low");
    expect(dishState({ available: true, portions: 10 })).toBe("available");
  });
});

describe("toneVars", () => {
  it("фон строки только у warn и risk", () => {
    expect(toneVars("warn").row).toBe("var(--urgency-warn-row)");
    expect(toneVars("risk").row).toBe("var(--urgency-risk-row)");
    expect(toneVars("critical").row).toBeUndefined();
  });
  it("текст на жёлтой заливке тёмный", () => {
    expect(toneVars("warn").onSolid).toBe("var(--on-urgency-warn-solid)");
    expect(toneVars("info").solid).toBe("var(--urgency-info-fg)");
  });
});
