import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createEvents } from "./events";

class FakeSource {
  static all: FakeSource[] = [];
  onopen: ((e: Event) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  closed = false;
  fns = new Map<string, (e: MessageEvent) => void>();
  constructor(public url: string) {
    FakeSource.all.push(this);
  }
  addEventListener(t: string, fn: (e: MessageEvent) => void) {
    this.fns.set(t, fn);
  }
  close() {
    this.closed = true;
  }
  emit(t: string, data: unknown) {
    this.fns.get(t)?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

const make = () =>
  createEvents({ create: (u) => new FakeSource(u), retryMs: 1000, silenceMs: 5000 });

describe("events", () => {
  beforeEach(() => {
    FakeSource.all = [];
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it("открывает одно соединение и перечитывает данные при подключении", () => {
    const ev = make();
    const resync = vi.fn();
    ev.onResync(resync);
    ev.start();
    ev.start();
    expect(FakeSource.all).toHaveLength(1);
    expect(ev.getStatus()).toBe("connecting");
    FakeSource.all[0].onopen?.(new Event("open"));
    expect(ev.getStatus()).toBe("online");
    expect(resync).toHaveBeenCalledTimes(1);
  });

  it("при ошибке показывает отсутствие связи и переподключается с паузой", () => {
    const ev = make();
    const resync = vi.fn();
    const statuses: string[] = [];
    ev.onResync(resync);
    ev.onStatus((s) => statuses.push(s));
    ev.start();
    FakeSource.all[0].onopen?.(new Event("open"));
    FakeSource.all[0].onerror?.(new Event("error"));
    expect(FakeSource.all[0].closed).toBe(true);
    expect(ev.getStatus()).toBe("offline");
    expect(FakeSource.all).toHaveLength(1);
    vi.advanceTimersByTime(1000);
    expect(FakeSource.all).toHaveLength(2);
    FakeSource.all[1].onopen?.(new Event("open"));
    expect(statuses).toEqual(["online", "offline", "online"]);
    expect(resync).toHaveBeenCalledTimes(2);
  });

  it("тишина дольше срока считается потерей связи, ping её продлевает", () => {
    const ev = make();
    ev.start();
    FakeSource.all[0].onopen?.(new Event("open"));
    vi.advanceTimersByTime(4000);
    FakeSource.all[0].emit("ping", {});
    vi.advanceTimersByTime(4000);
    expect(ev.getStatus()).toBe("online");
    vi.advanceTimersByTime(1500);
    expect(ev.getStatus()).toBe("offline");
  });

  it("доставляет события по типам, ping не доставляет", () => {
    const ev = make();
    const all = vi.fn();
    const stock = vi.fn();
    ev.onEvent(all);
    ev.onEvent(stock, ["stock.changed"]);
    ev.start();
    const s = FakeSource.all[0];
    s.emit("ping", {});
    s.emit("stock.changed", { product_ids: ["a"] });
    s.emit("order.accepted", { order_id: "o" });
    expect(all).toHaveBeenCalledTimes(2);
    expect(stock).toHaveBeenCalledWith({ type: "stock.changed", data: { product_ids: ["a"] } });
    expect(stock).toHaveBeenCalledTimes(1);
  });

  it("stop закрывает соединение и не переподключается", () => {
    const ev = make();
    ev.start();
    FakeSource.all[0].onerror?.(new Event("error"));
    ev.stop();
    vi.advanceTimersByTime(5000);
    expect(FakeSource.all).toHaveLength(1);
  });
});
