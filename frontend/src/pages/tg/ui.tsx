import { useEffect, useRef } from "react";
import type { CSSProperties, ReactNode } from "react";
import { getWebApp } from "./sdk";

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
  const mb = getWebApp()?.MainButton;
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

/** Системная кнопка «Назад» Telegram; показывается, пока передан обработчик. */
export function useBackButton(onBack: (() => void) | null): void {
  const cb = useRef(onBack);
  cb.current = onBack;
  const active = onBack !== null;
  const bb = getWebApp()?.BackButton;
  useEffect(() => {
    if (!bb || !active) return;
    const handler = () => cb.current?.();
    bb.onClick(handler);
    bb.show();
    return () => {
      bb.offClick(handler);
      bb.hide();
    };
  }, [bb, active]);
}
