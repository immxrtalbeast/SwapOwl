import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/types";
import { changePasswordError, validatePasswordChange } from "./validate";

describe("validatePasswordChange", () => {
  it("принимает корректную пару", () => {
    expect(validatePasswordChange("password1", "password22", "password22")).toBeNull();
  });
  it("требует текущий пароль", () => {
    expect(validatePasswordChange("", "password22", "password22")).toMatch(/текущий/);
  });
  it("длина считается в байтах: 8..72", () => {
    expect(validatePasswordChange("a", "1234567", "1234567")).toMatch(/8 до 72/);
    expect(validatePasswordChange("a", "я".repeat(37), "я".repeat(37))).toMatch(/8 до 72/); // 74 байта
    expect(validatePasswordChange("a", "я".repeat(4), "я".repeat(4))).toBeNull(); // 8 байт
  });
  it("новый пароль отличается от старого и совпадает с повтором", () => {
    expect(validatePasswordChange("password1", "password1", "password1")).toMatch(/отличаться/);
    expect(validatePasswordChange("password1", "password22", "password23")).toMatch(/не совпадают/);
  });
});

describe("changePasswordError", () => {
  it("переводит коды сервера", () => {
    expect(changePasswordError(new ApiError(401, "unauthorized", "x", {}))).toMatch(/неверен/);
    expect(changePasswordError(new ApiError(429, "too_many_attempts", "x", {}))).toMatch(/15 минут/);
    expect(changePasswordError(new ApiError(403, "forbidden", "x", {}))).toMatch(/вошедший/);
    expect(changePasswordError(new ApiError(400, "validation_error", "x", {}))).toMatch(/8 до 72/);
    expect(changePasswordError(new Error("сеть"))).toBe("сеть");
  });
});
