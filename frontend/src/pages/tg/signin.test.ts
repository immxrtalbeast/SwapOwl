import { describe, expect, it } from "vitest";
import { linkCodeErrorText, normalizeLinkInput } from "./signin";
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

describe("привязка по коду", () => {
  it("ввод как на экране: регистр, пробелы, дефис не важны", () => {
    expect(normalizeLinkInput("abcd-efgh")).toBe("ABCD-EFGH");
    expect(normalizeLinkInput("  abcd efgh ")).toBe("ABCD-EFGH");
    expect(normalizeLinkInput("ABCDEFGH")).toBe("ABCD-EFGH");
    expect(normalizeLinkInput("ABC")).toBeNull();
    expect(normalizeLinkInput("")).toBeNull();
    expect(normalizeLinkInput("ABCD-EFGH-IJ")).toBeNull();
  });
  it("тексты ошибок: неверный/просроченный/использованный код не различаются", () => {
    expect(linkCodeErrorText({ status: 400, code: "invalid_link_code" })).toContain("неверный, просрочен или уже использован");
    expect(linkCodeErrorText({ status: 429 })).toContain("Подождите 10 минут");
    expect(linkCodeErrorText({ status: 409 })).toContain("уже привязан к другому");
    expect(linkCodeErrorText({ status: 401 })).toContain("откройте его из бота");
    expect(linkCodeErrorText(new Error("boom"))).toContain("Не удалось привязать");
  });
});
