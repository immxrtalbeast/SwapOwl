import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/types";
import { classifyTgSignIn } from "./signin";

describe("classifyTgSignIn", () => {
  it("403 telegram_not_linked отдаёт id для привязки", () => {
    const e = new ApiError(403, "telegram_not_linked", "не привязан", { telegram_user_id: 1388227212 });
    expect(classifyTgSignIn(e)).toEqual({ kind: "not_linked", telegramUserId: "1388227212" });
  });
  it("401 — подпись не принята", () => {
    expect(classifyTgSignIn(new ApiError(401, "unauthorized", "x"))).toEqual({ kind: "rejected" });
  });
  it("404 (вход не включён) и сеть — обычный вход", () => {
    expect(classifyTgSignIn(new ApiError(404, "not_found", "x")).kind).toBe("unavailable");
    expect(classifyTgSignIn(new Error("Нет связи")).kind).toBe("unavailable");
  });
});
