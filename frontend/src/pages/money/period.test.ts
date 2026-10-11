import { describe, expect, it } from "vitest";
import { moneyPeriod } from "./period";

describe("moneyPeriod", () => {
  it("смена days меняет from, to остаётся", () => {
    const now = "2026-10-10T10:00:00Z";
    const week = moneyPeriod(now, 7);
    const month = moneyPeriod(now, 30);
    expect(week.from).toBe("2026-10-03T10:00:00.000Z");
    expect(month.from).toBe("2026-09-10T10:00:00.000Z");
    expect(week.to).toBe("2026-10-10T10:00:01.000Z");
    expect(month.to).toBe(week.to);
  });
});
