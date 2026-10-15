import type { ReactNode } from "react";

/** Направление стрелки от пометки к объекту. */
type Direction = "down" | "left" | "right";

const PATHS: Record<Direction, string> = {
  down: "M6 4 C 10 14, 22 18, 30 36 M30 36 L21 30 M30 36 L32 25",
  left: "M44 20 C 30 10, 14 14, 6 26 M6 26 L16 25 M6 26 L11 17",
  right: "M4 20 C 18 10, 34 14, 42 26 M42 26 L32 25 M42 26 L37 17",
};

/**
 * Пояснение к числу или действию: спокойная подпись --type-hand цвета --brand-ink.
 * Рукописный шрифт и стрелка убраны в FS-161 (спорили с остальной типографикой); стрелку можно вернуть
 * явным arrow — тогда она тонкая и короткая. Одна-две на экран; в таблицах, формах и ошибках не ставим.
 */
export function HandNote({ children, arrow = null }: { children: ReactNode; arrow?: Direction | null }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, font: "var(--type-hand)", color: "var(--brand-ink)" }}>
      <span>{children}</span>
      {arrow && (
        <svg className="so-arrow" width="28" height="24" viewBox="0 0 48 40" fill="none" aria-hidden="true">
          <path d={PATHS[arrow]} pathLength={1} stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      )}
    </span>
  );
}
