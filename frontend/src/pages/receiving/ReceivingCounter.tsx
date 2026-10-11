import { useReceiving } from "./useReceiving";

/** Счётчик неподтверждённых сроков в шапке; клик открывает приёмку. Нуль — ничего не показывает. */
export function ReceivingCounter() {
  const [state] = useReceiving();
  if (state.kind !== "ready" || state.rows.length === 0) return null;
  const n = state.rows.length;
  return (
    <a
      href="#/receiving"
      data-testid="receiving-counter"
      aria-label={`Неподтверждённых сроков: ${n}`}
      style={{ background: "#fff4d6", color: "#6b4e00", padding: "2px 8px", borderRadius: 4, fontSize: "0.85em", whiteSpace: "nowrap", textDecoration: "none" }}
    >
      <span aria-hidden="true">⏱ </span>
      {n} без подтверждённого срока
    </a>
  );
}
