import { afterEach, describe, expect, it, vi } from "vitest";
import { getWebApp, inTelegram, loadTelegramSdk } from "./sdk";

type Script = { src: string; async: boolean; onload: (() => void) | null; onerror: (() => void) | null };

function fakeDom() {
  const scripts: Script[] = [];
  const w: { Telegram?: { WebApp?: { ready(): void } } } = {};
  vi.stubGlobal("window", w);
  vi.stubGlobal("document", {
    createElement: () => ({ src: "", async: false, onload: null, onerror: null }) as Script,
    head: { appendChild: (s: Script) => { scripts.push(s); } },
  });
  return { scripts, w };
}

afterEach(() => vi.unstubAllGlobals());

describe("loadTelegramSdk", () => {
  it("грузит скрипт асинхронно, после onload getWebApp() видит SDK", async () => {
    const { scripts, w } = fakeDom();
    expect(getWebApp()).toBeNull();
    const p = loadTelegramSdk();
    expect(scripts).toHaveLength(1);
    expect(scripts[0]!.async).toBe(true);
    expect(scripts[0]!.src).toContain("telegram.org/js/telegram-web-app.js");
    w.Telegram = { WebApp: { ready() {} } };
    scripts[0]!.onload?.();
    await p;
    expect(getWebApp()).not.toBeNull();
  });

  it("недоступный telegram.org не ломает промис: работа вне Telegram", async () => {
    const { scripts } = fakeDom();
    const p = loadTelegramSdk();
    scripts[0]!.onerror?.();
    await expect(p).resolves.toBeUndefined();
    expect(getWebApp()).toBeNull();
  });
});

describe("inTelegram", () => {
  it("SDK без Telegram (platform unknown) — не Telegram: кнопки рисуются на странице", async () => {
    const { w } = fakeDom();
    expect(inTelegram()).toBe(false);
    w.Telegram = { WebApp: { ready() {}, platform: "unknown" } as never };
    expect(inTelegram()).toBe(false);
    w.Telegram = { WebApp: { ready() {}, platform: "ios" } as never };
    expect(inTelegram()).toBe(true);
  });
});
