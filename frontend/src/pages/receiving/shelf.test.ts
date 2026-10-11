import { describe, expect, it } from "vitest";
import { currentShelfHours, datePatch, formatShelf } from "./shelf";

describe("shelf", () => {
  it("текущий срок хранения в часах", () => {
    expect(currentShelfHours("2026-10-12T10:00:00Z", "2026-10-15T10:00:00Z")).toBe(72);
  });
  it("подпись срока", () => {
    expect(formatShelf(72)).toBe("3 дн");
    expect(formatShelf(36)).toBe("36 ч");
    expect(formatShelf(12)).toBe("12 ч");
  });
  it("datePatch принимает только полную дату", () => {
    expect(datePatch("2026-10-20")).toEqual({ expires_on: "2026-10-20" });
    expect(datePatch("")).toBeNull();
    expect(datePatch("2026-10")).toBeNull();
  });
});
