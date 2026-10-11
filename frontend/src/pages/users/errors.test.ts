import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/types";
import { createUserError, deleteUserError } from "./errors";

describe("deleteUserError", () => {
  it("409 объясняет, что последнего администратора удалить нельзя", () => {
    expect(deleteUserError(new ApiError(409, "conflict", "the last admin cannot be deleted"))).toMatch(/последнего администратора/);
  });
  it("прочие ошибки — по сообщению", () => {
    expect(deleteUserError(new ApiError(500, "internal", "boom"))).toBe("boom");
  });
});

describe("createUserError", () => {
  it("409 — логин занят", () => {
    expect(createUserError(new ApiError(409, "conflict", "x"))).toMatch(/логином/);
  });
});
