import { describe, expect, it } from "vitest";
import { formatRub, reasonLabel } from "./format";

describe("formatRub", () => {
  it("форматирует копейки в рубли с неразрывными пробелами", () => {
    expect(formatRub(1234000)).toBe("12 340 ₽");
    expect(formatRub(0)).toBe("0 ₽");
    expect(formatRub(99)).toBe("0,99 ₽");
    expect(formatRub(1234050)).toBe("12 340,50 ₽");
    expect(formatRub(100000000)).toBe("1 000 000 ₽");
  });
  it("подписывает причины", () => {
    expect(reasonLabel("expired")).toBe("истёк срок");
    expect(reasonLabel("spoiled")).toBe("spoiled");
  });
});
