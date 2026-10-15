import { describe, expect, it } from "vitest";
import type { Alert, Batch, ProductStock } from "../../api/types";
import { buildRows, changedKeys, productStateLabel, signature } from "./view";

const product = (id: string): ProductStock => ({ id, name: id, unit: "g", available: { qty: 1000, unit: "g" }, batches_count: 2, nearest_expires_at: null });
const batch = (id: number, product_id: string, hours: number, over: Partial<Batch> = {}): Batch => ({
  id, product_id, delivery_id: "d", delivery_created_at: "2026-10-11T10:00:00Z",
  quantity: { qty: 500, unit: "g" }, remaining: { qty: 500, unit: "g" },
  received_at: "2026-10-11T10:00:00Z", expires_at: new Date(Date.parse("2026-10-11T10:00:00Z") + hours * 3600_000).toISOString(),
  time_left_seconds: hours * 3600, status: "ok", expiry_source: "delivery", ...over,
});
const alert = (batch_id: number, status: Alert["status"], doomed: boolean): Alert => ({
  batch_id, product_id: "salmon", product_name: "Лосось", remaining: { qty: 500, unit: "g" }, expires_at: "", time_left_seconds: 0, status, doomed,
});

describe("buildRows", () => {
  const products = [product("salmon"), product("milk")];
  const batches = [batch(2, "salmon", 48), batch(1, "salmon", 5), batch(3, "milk", 72)];

  it("продукт — по худшему состоянию партий: «спишется» выше «истекает»", () => {
    const rows = buildRows(products, batches, [alert(1, "expiring", true), alert(2, "expiring", false)], new Set());
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ kind: "product", state: "doomed" });
    expect(rows[1]).toMatchObject({ kind: "product", state: "ok" });
  });

  it("раскрытый продукт выводит партии по сроку", () => {
    const rows = buildRows(products, batches, [alert(1, "expiring", false)], new Set(["salmon"]));
    expect(rows.map((r) => r.key)).toEqual(["p:salmon", "b:1", "b:2", "p:milk"]);
    expect(rows[1]).toMatchObject({ kind: "batch", state: "expiring" });
  });

  it("просроченная и израсходованная партии", () => {
    const rows = buildRows(
      [product("salmon")],
      [batch(1, "salmon", -1, { status: "expired" }), batch(2, "salmon", 5, { remaining: { qty: 0, unit: "g" } })],
      [alert(1, "expired", false)],
      new Set(["salmon"]),
    );
    expect(rows.slice(1).map((r) => r.state)).toEqual(["expired", "spent"]);
  });
});

describe("changedKeys", () => {
  const products = [product("salmon")];
  it("первая загрузка ничего не подсвечивает", () => {
    const s = signature(buildRows(products, [batch(1, "salmon", 5)], [], new Set()));
    expect(changedKeys(null, s).size).toBe(0);
  });
  it("подсвечивает партию с изменившимся остатком и новую партию", () => {
    const open = new Set(["salmon"]);
    const prev = signature(buildRows(products, [batch(1, "salmon", 5)], [], open));
    const next = signature(buildRows(products, [batch(1, "salmon", 5, { remaining: { qty: 100, unit: "g" } }), batch(2, "salmon", 9)], [], open));
    expect([...changedKeys(prev, next)].sort()).toEqual(["b:1", "b:2"]);
  });
});

describe("productStateLabel", () => {
  it("у продукта с просроченной партией — «Есть просрочка», а не «Просрочена» рядом со свежей датой", () => {
    expect(productStateLabel("expired")).toBe("Есть просрочка");
    expect(productStateLabel("doomed")).toBe("Часть спишется");
    expect(productStateLabel("ok")).toBeUndefined();
    expect(productStateLabel("expiring")).toBeUndefined();
  });
});
