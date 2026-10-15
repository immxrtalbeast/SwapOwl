import { describe, expect, it } from "vitest";
import { OUTBOX_KEY, enqueue, flush, loadQueue, remove } from "./outbox";
import type { Queued, Store } from "./outbox";

function memStore(): Store & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return { data, getItem: (k) => data.get(k) ?? null, setItem: (k, v) => void data.set(k, v) };
}
const q = (id: string): Queued => ({ id, at: "2026-10-15T11:00:00Z", items: [{ dish_id: "steak", portions: 1 }] });
const net = (e: unknown) => e instanceof Error && e.message === "net";

describe("очередь заказов без связи", () => {
  it("кладёт заказ один раз: двойное нажатие с тем же id — один заказ", () => {
    const s = memStore();
    enqueue(s, q("a"));
    enqueue(s, q("a"));
    expect(loadQueue(s).map((x) => x.id)).toEqual(["a"]);
  });

  it("переживает перезагрузку: читается из хранилища", () => {
    const s = memStore();
    enqueue(s, q("a"));
    expect(loadQueue(s)).toHaveLength(1);
    expect(s.data.get(OUTBOX_KEY)).toContain('"id":"a"');
  });

  it("мусор в хранилище не ломает: пусто", () => {
    const s = memStore();
    s.data.set(OUTBOX_KEY, "{не json");
    expect(loadQueue(s)).toEqual([]);
    s.data.set(OUTBOX_KEY, JSON.stringify([{ id: "", items: [] }, { id: "ok", at: "x", items: [{ dish_id: "d", portions: 2 }] }]));
    expect(loadQueue(s).map((x) => x.id)).toEqual(["ok"]);
  });

  it("при потере связи останавливается и ничего не теряет; потом доезжает по порядку", async () => {
    const s = memStore();
    enqueue(s, q("a"));
    enqueue(s, q("b"));
    const sent: string[] = [];
    let online = false;
    const send = async (id: string) => {
      if (!online) throw new Error("net");
      sent.push(id);
      return { status: "accepted" as const, replayed: false };
    };
    expect(await flush(s, send, net)).toEqual([]);
    expect(loadQueue(s)).toHaveLength(2);
    online = true;
    const out = await flush(s, send, net);
    expect(sent).toEqual(["a", "b"]);
    expect(out.map((o) => o.kind)).toEqual(["sent", "sent"]);
    expect(loadQueue(s)).toEqual([]);
  });

  it("ответ потерян, а заказ уже принят: повтор с тем же id возвращает replayed, дубля нет", async () => {
    const s = memStore();
    enqueue(s, q("a"));
    const server = new Map<string, number>(); // id → сколько раз реально создан
    const send = async (id: string) => {
      const had = server.has(id);
      server.set(id, (server.get(id) ?? 0) + (had ? 0 : 1));
      return { status: "accepted" as const, replayed: had };
    };
    await send("a"); // первая отправка дошла, ответ потерян
    const out = await flush(s, send, net);
    expect(server.get("a")).toBe(1);
    expect(out).toEqual([{ kind: "sent", id: "a", accepted: true, replayed: true }]);
  });

  it("ошибка API снимает заказ с очереди, остальные идут дальше", async () => {
    const s = memStore();
    enqueue(s, q("bad"));
    enqueue(s, q("ok"));
    const send = async (id: string) => {
      if (id === "bad") throw new Error("validation_error");
      return { status: "rejected" as const, replayed: false };
    };
    const out = await flush(s, send, net);
    expect(out).toEqual([
      { kind: "dropped", id: "bad", reason: "validation_error" },
      { kind: "sent", id: "ok", accepted: false, replayed: false },
    ]);
    expect(loadQueue(s)).toEqual([]);
  });

  it("remove убирает только свой id", () => {
    const s = memStore();
    enqueue(s, q("a"));
    enqueue(s, q("b"));
    expect(remove(s, "a").map((x) => x.id)).toEqual(["b"]);
  });
});
