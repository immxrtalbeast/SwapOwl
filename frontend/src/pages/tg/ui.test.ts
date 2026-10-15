import { describe, expect, it, vi } from "vitest";
import { pushBack } from "./ui";
import type { TgButton } from "./sdk";

function fakeButton() {
  let handler: (() => void) | null = null;
  const bb: TgButton & { press(): void; visible: boolean } = {
    visible: false,
    show() { bb.visible = true; },
    hide() { bb.visible = false; },
    onClick(cb) { handler = cb; },
    offClick(cb) { if (handler === cb) handler = null; },
    press() { handler?.(); },
  };
  return bb;
}

describe("стек «Назад» Telegram", () => {
  it("срабатывает только верхний обработчик, кнопка живёт, пока стек не пуст", () => {
    const bb = fakeButton();
    const tab = vi.fn();
    const form = vi.fn();
    const popTab = pushBack(bb, { cb: { current: tab }, priority: 0 });
    const popForm = pushBack(bb, { cb: { current: form }, priority: 1 });
    expect(bb.visible).toBe(true);
    bb.press();
    expect(form).toHaveBeenCalledTimes(1);
    expect(tab).not.toHaveBeenCalled();
    popForm();
    expect(bb.visible).toBe(true); // кнопка осталась для вкладки
    bb.press();
    expect(tab).toHaveBeenCalledTimes(1);
    popTab();
    expect(bb.visible).toBe(false);
  });

  it("вкладка, смонтированная позже вложенного экрана, его не перекрывает", () => {
    const bb = fakeButton();
    const tab = vi.fn();
    const form = vi.fn();
    const popForm = pushBack(bb, { cb: { current: form }, priority: 1 });
    const popTab = pushBack(bb, { cb: { current: tab }, priority: 0 });
    bb.press();
    expect(form).toHaveBeenCalledTimes(1);
    expect(tab).not.toHaveBeenCalled();
    popTab();
    popForm();
  });
});
