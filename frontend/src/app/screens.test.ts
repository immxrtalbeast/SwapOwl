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
  it("«Сегодня» — первый пункт меню, но стартовый экран — склад", () => {
    expect(SCREENS[0].id).toBe("today");
    expect(screenFromHash("#/today")).toBe("today");
    expect(screenFromHash("")).toBe("stock");
  });
  it("есть экран приёмки", () => {
    expect(SCREENS.find((s) => s.id === "receiving")?.title).toBe("Приёмка");
    expect(screenFromHash("#/receiving")).toBe("receiving");
  });
  it("есть экран аккаунта, доступный любой роли", () => {
    const a = SCREENS.find((s) => s.id === "account");
    expect(a).toBeDefined();
    expect(a?.minRole).toBeUndefined();
    expect(screenFromHash("#/account")).toBe("account");
  });
  it("пункт «Сеть» зависит от hub", () => {
    expect(SCREENS.find((s) => s.id === "network")?.needsHub).toBe(true);
    expect(SCREENS.filter((s) => s.id !== "network").every((s) => !s.needsHub)).toBe(true);
    expect(screenFromHash("#/network")).toBe("network");
  });
  it("экран пользователей и ключей только для администратора", () => {
    expect(SCREENS.find((s) => s.id === "users")?.minRole).toBe("admin");
  });
});
