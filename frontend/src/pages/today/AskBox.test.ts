import { describe, expect, it } from "vitest";
import { ASK_EXAMPLES } from "./AskBox";

describe("AskBox", () => {
  it("подсказки — вопросы, которые понимает помощник, без повторов и в пределах лимита API", () => {
    expect(new Set(ASK_EXAMPLES).size).toBe(ASK_EXAMPLES.length);
    for (const q of ASK_EXAMPLES) {
      expect(q.length).toBeGreaterThan(0);
      expect(q.length).toBeLessThanOrEqual(300);
    }
  });
});
