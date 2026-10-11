import { describe, expect, it } from "vitest";
import { formatStockout, supplyUrgencyView } from "./urgency";

describe("supply urgency", () => {
  it("now использует тон риска, soon — предупреждения", () => {
    expect(supplyUrgencyView("now").color).toContain("urgency-risk");
    expect(supplyUrgencyView("soon").color).toContain("urgency-warn");
    expect(supplyUrgencyView("planned").label).toBe("По плану");
  });

  it("форматирует время до обнуления", () => {
    const now = Date.parse("2026-10-10T10:00:00Z");
    expect(formatStockout(null, now)).toBe("расхода нет");
    expect(formatStockout("2026-10-10T15:00:00Z", now)).toBe("через 5 ч");
    expect(formatStockout("2026-10-13T10:00:00Z", now)).toBe("через 3 сут.");
    expect(formatStockout("2026-10-10T09:00:00Z", now)).toBe("уже закончился");
  });
});
