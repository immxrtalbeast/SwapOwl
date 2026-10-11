import { describe, expect, it } from "vitest";
import { SCREENS, screenFromHash } from "./screens";

describe("screenFromHash", () => {
  it("находит экран по адресу, неизвестное ведёт на склад", () => {
    expect(screenFromHash("#/orders")).toBe("orders");
    expect(screenFromHash("#hot-offers")).toBe("hot-offers");
    expect(screenFromHash("#/hot-offers?product_id=milk")).toBe("hot-offers");
    expect(screenFromHash("")).toBe("stock");
    expect(screenFromHash("#/nope")).toBe("stock");
  });
});

describe("SCREENS", () => {
  it("экран пользователей и ключей только для администратора", () => {
    expect(SCREENS.find((s) => s.id === "users")?.minRole).toBe("admin");
  });
});
