import { describe, expect, it } from "vitest";
import { serialLoader } from "./useLive";

describe("serialLoader", () => {
  it("не запускает загрузки внахлёст и повторяет её после завершения", async () => {
    const releases: Array<() => void> = [];
    let active = 0;
    let maxActive = 0;
    let started = 0;
    const run = serialLoader(
      () =>
        new Promise<void>((resolve) => {
          started++;
          active++;
          maxActive = Math.max(maxActive, active);
          releases.push(() => {
            active--;
            resolve();
          });
        }),
    );
    const first = run();
    void run();
    void run(); // два запроса во время загрузки сводятся к одному повтору
    expect(started).toBe(1);
    releases[0]();
    await Promise.resolve();
    await Promise.resolve();
    expect(started).toBe(2);
    releases[1]();
    await first;
    expect(started).toBe(2);
    expect(maxActive).toBe(1);
  });

  it("ошибка загрузки не ломает последующие", async () => {
    let n = 0;
    const run = serialLoader(async () => {
      n++;
      if (n === 1) throw new Error("boom");
    });
    await run();
    await run();
    expect(n).toBe(2);
  });
});
