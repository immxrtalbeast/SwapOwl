import { describe, expect, it } from "vitest";
import { buildLabel, labelBatchIdFromHash, needsOpenFirst } from "./labelData";

describe("labelBatchIdFromHash", () => {
  it("разбирает номер партии", () => {
    expect(labelBatchIdFromHash("#/label/42")).toBe(42);
    expect(labelBatchIdFromHash("#label/7/")).toBe(7);
    expect(labelBatchIdFromHash("#/label/x")).toBeNull();
    expect(labelBatchIdFromHash("#/stock")).toBeNull();
  });
});

describe("buildLabel", () => {
  const base = { product: "Молоко", batchId: 5, expiresAt: "2026-10-14T11:00:00Z", nowMs: Date.parse("2026-10-12T11:00:00Z"), timeZone: "Europe/Moscow", login: "anna", origin: "https://s.example" };
  it("вскрытая часть берёт свои даты", () => {
    const l = buildLabel({ ...base, openedAt: "2026-10-12T09:00:00Z" });
    expect(l.openedStamp).toBe("12.10 12:00");
    expect(l.untilStamp).toBe("14.10 14:00");
    expect(l.qrUrl).toBe("https://s.example/#/stock?batch=5");
  });
  it("без вскрытия «вскрыто» — момент печати", () => {
    expect(buildLabel(base).openedStamp).toBe("12.10 14:00");
  });
});

describe("needsOpenFirst", () => {
  it("невскрытая партия со сроком после вскрытия требует «Вскрыл»", () => {
    expect(needsOpenFirst(null, 48)).toBe(true);
    expect(needsOpenFirst(undefined, 48)).toBe(true);
  });
  it("вскрытая или без срока после вскрытия — можно печатать", () => {
    expect(needsOpenFirst("2026-10-12T09:00:00Z", 48)).toBe(false);
    expect(needsOpenFirst(null, null)).toBe(false);
    expect(needsOpenFirst(null, undefined)).toBe(false);
  });
});
