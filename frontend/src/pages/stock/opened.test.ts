import { describe, expect, it } from "vitest";
import { formatOpenedStamp, openConfirmText, openedMark, previewOpenedExpiry } from "./opened";

const NOW = Date.parse("2026-10-12T11:00:00Z"); // 14:00 по Москве

describe("opened", () => {
  it("stamps in the restaurant timezone", () => {
    expect(formatOpenedStamp("2026-10-12T11:00:00Z", "Europe/Moscow")).toBe("12.10 14:00");
    expect(formatOpenedStamp("2026-10-12T21:30:00Z", "Europe/Moscow")).toBe("13.10 00:30");
    expect(openedMark("2026-10-12T11:00:00Z", "Europe/Moscow")).toBe("вскрыто 12.10 14:00");
  });
  it("confirmation text names the product, hours and the end of the shelf life", () => {
    expect(openConfirmText("Молоко", 48, NOW, "2026-10-22T11:00:00Z", "Europe/Moscow")).toBe("молоко годно 48 ч после вскрытия — до 14.10 14:00");
  });
  it("never previews a later date than the batch expiry", () => {
    expect(previewOpenedExpiry(NOW, 48, "2026-10-13T11:00:00Z")).toBe(Date.parse("2026-10-13T11:00:00Z"));
    expect(previewOpenedExpiry(NOW, 48, "2026-10-22T11:00:00Z")).toBe(NOW + 48 * 3_600_000);
  });
});
