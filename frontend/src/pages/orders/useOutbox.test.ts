import { describe, expect, it } from "vitest";
import { ApiError, NetworkError } from "../../api/types";
import { isTemporary } from "./useOutbox";

describe("isTemporary", () => {
  it("нет сети, перезапуск сервера, перегрузка и истёкшая сессия — заказ остаётся в очереди", () => {
    expect(isTemporary(new NetworkError("нет связи"))).toBe(true);
    for (const s of [401, 408, 429, 500, 502, 503, 504]) expect(isTemporary(new ApiError(s, "x", "x")), String(s)).toBe(true);
  });
  it("окончательные ошибки (валидация, конфликт тела) — нет", () => {
    for (const s of [400, 403, 404, 409, 422]) expect(isTemporary(new ApiError(s, "x", "x")), String(s)).toBe(false);
    expect(isTemporary(new Error("странное"))).toBe(false);
  });
});
