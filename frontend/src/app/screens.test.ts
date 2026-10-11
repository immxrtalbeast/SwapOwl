import { describe, expect, it } from "vitest";
import { screenFromHash } from "./screens";

describe("screenFromHash", () => {
  it("находит экран по адресу, неизвестное ведёт на склад", () => {
    expect(screenFromHash("#/orders")).toBe("orders");
    expect(screenFromHash("#hot-offers")).toBe("hot-offers");
    expect(screenFromHash("")).toBe("stock");
    expect(screenFromHash("#/nope")).toBe("stock");
  });
});
