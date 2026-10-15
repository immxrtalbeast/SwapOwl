import { useEffect, useRef } from "react";
import type { CSSProperties, ReactNode } from "react";
import { TextButton } from "../../components";
import { getWebApp, inTelegram } from "./sdk";
import type { TgButton } from "./sdk";

export const section: CSSProperties = { display: "grid", gap: 12, minWidth: 0 };
export const heading: CSSProperties = { margin: 0, font: "var(--type-h2)" };
export const muted: CSSProperties = { margin: 0, font: "var(--type-small)", color: "var(--text-muted)" };
export const errorText: CSSProperties = { margin: 0, color: "var(--urgency-critical-fg)", font: "var(--type-small)" };
export const listReset: CSSProperties = { listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 8 };

export function TgCard({ children, accent, testid }: { children: ReactNode; accent?: boolean; testid?: string }) {
  return (
    <div
      data-testid={testid}
      style={{ display: "grid", gap: 6, padding: 14, borderRadius: 12, minWidth: 0, overflowWrap: "anywhere", background: "var(--surface)", border: `2px solid ${accent ? "var(--primary)" : "var(--border)"}` }}
    >
      {children}
    </div>
  );
}

/**
 * Главная кнопка Telegram (MainButton) для главного действия экрана; вне Telegram — обычная кнопка внизу содержимого.
 * Возвращает признак «кнопка рисуется на странице».
 */
export function useMainButton(text: string | null, onClick: () => void): boolean {
  const cb = useRef(onClick);
  cb.current = onClick;
  const mb = inTelegram() ? getWebApp()?.MainButton : undefined;
  useEffect(() => {
    if (!mb || !text) return;
    const handler = () => cb.current();
    mb.setText(text);
    mb.onClick(handler);
    mb.show();
    return () => {
      mb.offClick(handler);
      mb.hide();
    };
  }, [mb, text]);
  return !mb && !!text;
}

interface BackEntry {
  cb: { current: (() => void) | null };
  priority: number;
}

// Системная кнопка одна, а подписчиков несколько (вкладка и её вложенный экран): вызывается только верхний —
// с наибольшим приоритетом, при равных — самый поздний.
const backStack: BackEntry[] = [];

const dispatchBack = () => {
  let top: BackEntry | undefined;
  for (const e of backStack) if (!top || e.priority >= top.priority) top = e;
  top?.cb.current?.();
};

/** Ставит обработчик в стек «Назад»; возвращает снятие. Кнопка показывается, пока стек не пуст. */
export function pushBack(bb: TgButton, entry: BackEntry): () => void {
  if (backStack.length === 0) {
    bb.onClick(dispatchBack);
    bb.show();
  }
  backStack.push(entry);
  return () => {
    backStack.splice(backStack.indexOf(entry), 1);
    if (backStack.length === 0) {
      bb.offClick(dispatchBack);
      bb.hide();
    }
  };
}

/**
 * Системная кнопка «Назад» Telegram; показывается, пока передан обработчик. Обработчики образуют стек:
 * срабатывает только верхний (вложенный экран перекрывает вкладку — у неё больший priority; вкладке отдают 0).
 */
export function useBackButton(onBack: (() => void) | null, priority = 1): void {
  const cb = useRef(onBack);
  cb.current = onBack;
  const active = onBack !== null;
  const bb = inTelegram() ? getWebApp()?.BackButton : undefined;
  useEffect(() => {
    if (!bb || !active) return;
    return pushBack(bb, { cb, priority });
  }, [bb, active, priority]);
}

/** «Назад» на странице: вне Telegram системной кнопки нет, а вложенный экран иначе не закрыть. */
export function InlineBack({ onBack }: { onBack: () => void }) {
  if (inTelegram()) return null;
  return <TextButton onClick={onBack} style={{ justifySelf: "start" }}>← Назад</TextButton>;
}
