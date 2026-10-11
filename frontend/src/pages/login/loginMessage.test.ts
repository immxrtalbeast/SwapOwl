import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/types";
import { loginErrorMessage } from "./loginMessage";

describe("loginErrorMessage", () => {
  it("401 — неверные данные", () => {
    expect(loginErrorMessage(new ApiError(401, "unauthorized", "x"))).toContain("Неверный логин или пароль");
  });
  it("429 too_many_attempts — блокировка", () => {
    expect(loginErrorMessage(new ApiError(429, "too_many_attempts", "x"))).toContain("Слишком много");
  });
  it("сетевая ошибка", () => {
    expect(loginErrorMessage(new TypeError("fetch failed"))).toContain("не ответил");
  });
});
