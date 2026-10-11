import { describe, expect, it } from "vitest";
import { batchLabel } from "./batch";

describe("номер партии в ленте", () => {
  it("форматирует как в карточке горячего предложения", () => {
    expect(batchLabel(17)).toBe("партия №17");
    expect(batchLabel(1)).toBe("партия №1");
  });
});
