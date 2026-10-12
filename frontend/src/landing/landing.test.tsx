import { renderToStaticMarkup } from "react-dom/server";
import { MantineProvider } from "@mantine/core";
import { describe, expect, it } from "vitest";
import { Landing } from "./Landing";
import { benchNumbers, pickFirstBatch, pickNearestProduct } from "./data";
import { isLandingLocation } from "./route";
import type { Batch, ProductStock } from "../api/types";

const q = (qty: number) => ({ qty, unit: "g" as const });
const batch = (id: number, exp: string, status: "ok" | "expired" = "ok"): Batch => ({
  id, product_id: "p", delivery_id: "d", delivery_created_at: exp, quantity: q(100), remaining: q(50),
  received_at: exp, expires_at: exp, time_left_seconds: 1, status, expiry_source: "delivery",
});

describe("лендинг", () => {
  it("корень без хэша — лендинг, хэш-маршрут — приложение", () => {
    expect(isLandingLocation({ pathname: "/", hash: "" })).toBe(true);
    expect(isLandingLocation({ pathname: "/", hash: "#/stock" })).toBe(false);
    expect(isLandingLocation({ pathname: "/", hash: "#connect" })).toBe(true);
    expect(isLandingLocation({ pathname: "/", hash: "#/" })).toBe(true);
    expect(isLandingLocation({ pathname: "/x", hash: "" })).toBe(false);
  });
  it("кнопки ведут в демо, «Как подключить» — к шагам; подвал-концепт", () => {
    const out = renderToStaticMarkup(<MantineProvider><Landing /></MantineProvider>);
    expect(out.match(/href="\/#\/stock"/g)?.length).toBe(2);
    expect(out).toContain('href="#connect"');
    expect(out).toContain('id="connect"');
    expect(out).toContain("Три способа начать");
    expect(out).toContain("Работает самостоятельно");
    expect(out).toContain("Интеграции по API");
    expect(out).not.toContain("Работает поверх iiko");
    expect(out).toContain("Вручную");
    expect(out).toContain("Файл накладной");
    expect(out).toContain("iiko — пример");
    expect(out).toContain("7,6 → 0,1%");
    expect(out).toContain("Симулятор, 20 прогонов, допущения — в документации.");
    expect(out).toContain("Сделано по кейсу «Флагмана» · UMIRHack 2026");
    expect(out).not.toContain("ld-bench");
  });
  it("первая истекающая партия и продукт с ближайшим сроком", () => {
    expect(pickFirstBatch([batch(1, "2026-01-03T00:00:00Z"), batch(2, "2026-01-02T00:00:00Z"), batch(3, "2026-01-01T00:00:00Z", "expired")])?.id).toBe(2);
    const p = (id: string, e: string | null): ProductStock => ({ id, name: id, unit: "g", available: q(10), batches_count: 1, nearest_expires_at: e });
    expect(pickNearestProduct([p("a", null), p("b", "2026-01-05T00:00:00Z"), p("c", "2026-01-04T00:00:00Z")])?.id).toBe("c");
    expect(pickNearestProduct([p("a", null)])).toBeNull();
  });
  it("bench: нет результата — нет цифр (правило 2.7)", () => {
    expect(benchNumbers(null)).toBeNull();
    expect(benchNumbers({ available: false })).toBeNull();
    expect(benchNumbers({ available: true, runs: 20 })).toBeNull();
    expect(benchNumbers({ available: true, runs: 20, waste_reduction_vs_random_pct: 31, waste_reduction_vs_nearest_pct: 12 })).toEqual({ runs: 20, vsRandom: 31, vsNearest: 12 });
  });
});
