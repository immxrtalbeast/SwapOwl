import { describe, expect, it } from "vitest";
import type { Alert } from "../../api/types";
import { alertUrgency, countAlerts, urgencyView } from "./urgency";

const base: Alert = {
  batch_id: 1, product_id: "p", product_name: "Лосось", remaining: { qty: 500, unit: "g" },
  expires_at: "2026-10-10T12:00:00Z", time_left_seconds: 3600, status: "expiring", doomed: false,
};

describe("шкала срочности ленты", () => {
  it("выбирает тон по статусу и doomed", () => {
    expect(alertUrgency(base)).toBe("expiring");
    expect(alertUrgency({ ...base, doomed: true })).toBe("doomed");
    expect(alertUrgency({ ...base, status: "expired", doomed: true })).toBe("expired");
  });
  it("у тонов разные метки и цвета", () => {
    const views = (["expiring", "doomed", "expired"] as const).map(urgencyView);
    expect(new Set(views.map((v) => v.label)).size).toBe(3);
    expect(new Set(views.map((v) => v.solid)).size).toBe(3);
  });
  it("считает счётчики по тонам", () => {
    const list = [base, base, { ...base, doomed: true }, { ...base, status: "expired" as const }];
    expect(countAlerts(list)).toEqual({ expiring: 2, doomed: 1, expired: 1 });
    expect(countAlerts([])).toEqual({ expiring: 0, doomed: 0, expired: 0 });
  });
});
