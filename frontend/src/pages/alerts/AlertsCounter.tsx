import { useAlerts } from "./useAlerts";
import { countAlerts, urgencyView } from "./urgency";

function Chip({ urgency, n, text }: { urgency: "expiring" | "doomed" | "expired"; n: number; text: string }) {
  const v = urgencyView(urgency);
  return (
    <span data-urgency={urgency} style={{ background: v.background, color: v.color, padding: "2px 8px", borderRadius: 4, fontSize: "0.85em", whiteSpace: "nowrap" }}>
      <span aria-hidden="true">{v.icon} </span>
      {n} {text}
    </span>
  );
}

/** Счётчик предупреждений в слоте шапки: «N истекают», «N спишутся», «N просрочены»; клик открывает ленту. */
export function AlertsCounter() {
  const [state] = useAlerts();
  if (state.kind !== "ready") return null;
  const c = countAlerts(state.alerts);
  const total = c.expiring + c.doomed + c.expired;
  return (
    <a href="#/alerts" data-testid="alerts-counter" aria-label={total === 0 ? "Предупреждений нет" : `Предупреждения: ${total}`} style={{ display: "inline-flex", gap: 6, textDecoration: "none" }}>
      {total === 0 && <span style={{ color: "var(--text-muted, #525a68)" }}>Предупреждений нет</span>}
      {c.expiring > 0 && <Chip urgency="expiring" n={c.expiring} text="истекают" />}
      {c.doomed > 0 && <Chip urgency="doomed" n={c.doomed} text="спишутся" />}
      {c.expired > 0 && <Chip urgency="expired" n={c.expired} text="просрочены" />}
    </a>
  );
}
