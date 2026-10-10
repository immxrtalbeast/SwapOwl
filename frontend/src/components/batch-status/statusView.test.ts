import { describe, expect, it } from "vitest";
import { statusView } from "./statusView";

describe("statusView", () => {
  it("даёт подписи для трёх статусов", () => {
    expect(statusView("ok").label).toBe("Годна");
    expect(statusView("expiring").label).toBe("Истекает");
    expect(statusView("expired").label).toBe("Просрочена");
  });
  it("цвета у статусов различаются", () => {
    const colors = new Set(["ok", "expiring", "expired"].map((s) => statusView(s).color));
    expect(colors.size).toBe(3);
  });
  it("неизвестный статус показывается как есть", () => {
    expect(statusView("weird").label).toBe("weird");
  });
});
