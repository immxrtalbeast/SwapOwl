import { formatQty, formatTimeLeft } from "../stock/format";
import { useAlerts } from "./useAlerts";
import { hotOffersUrl } from "../hot-offers/query";
import { batchLabel } from "./batch";
import { Card, EmptyState, ErrorState, LoadingState, ScreenTitle, UrgencyBadge } from "../../components";
import { BATCH_STATES, toneVars } from "../../lib/urgency";
import { alertUrgency } from "./urgency";

const cell = { padding: "8px 12px" } as const;

export function AlertsPage() {
  const [state, reload] = useAlerts();
  return (
    <section>
      <ScreenTitle>Предупреждения</ScreenTitle>
      {state.kind === "loading" && <LoadingState label="Загрузка предупреждений" />}
      {state.kind === "error" && <ErrorState what="предупреждения" reason={state.message} onRetry={() => void reload()} />}
      {state.kind === "ready" && state.alerts.length === 0 && (
        <EmptyState title="Предупреждений нет" text="Предупреждений нет: истекающих и недавно просроченных партий нет." />
      )}
      {state.kind === "ready" && state.alerts.length > 0 && (
        <Card flush>
          <div style={{ overflowX: "auto", maxWidth: "100%" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", font: "var(--type-body)", fontVariantNumeric: "tabular-nums" }}>
              <caption style={{ position: "absolute", width: 1, height: 1, overflow: "hidden", clip: "rect(0 0 0 0)" }}>Лента предупреждений по партиям</caption>
              <thead>
                <tr style={{ background: "var(--surface-sunken)", font: "var(--type-caption)", textTransform: "uppercase", letterSpacing: ".04em" }}>
                  <th scope="col" align="left" style={cell}>Продукт</th>
                  <th scope="col" align="right" style={cell}>Количество</th>
                  <th scope="col" align="left" style={cell}>Срок</th>
                  <th scope="col" align="left" style={cell}>Состояние</th>
                  <th scope="col" align="left" style={cell}>Что делать</th>
                </tr>
              </thead>
              <tbody>
                {state.alerts.map((a) => {
                  const u = alertUrgency(a);
                  const def = BATCH_STATES[u];
                  return (
                    <tr key={a.batch_id} data-urgency={u} style={{ background: toneVars(def.tone).row, borderTop: "1px solid var(--border)", height: 44 }}>
                      <td style={cell}>
                        {a.product_name} <span style={{ font: "var(--type-num)", color: "var(--text-subtle)" }}>{batchLabel(a.batch_id)}</span>
                      </td>
                      <td align="right" style={{ ...cell, font: "var(--type-num)", fontWeight: u === "doomed" ? 700 : 400, textDecoration: u === "expired" ? "line-through" : "none" }}>
                        {formatQty(a.remaining)}
                      </td>
                      <td style={cell} title={new Date(a.expires_at).toLocaleString("ru-RU")}>{formatTimeLeft(a.time_left_seconds)}</td>
                      <td style={cell}><UrgencyBadge def={def} /></td>
                      <td style={cell}>
                        {a.status === "expiring" && (
                          <a href={hotOffersUrl(a.product_id)} style={{ font: "var(--type-button)", color: "var(--primary)", textDecoration: "none" }}>Блюда, которые спасают</a>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </Card>
      )}
    </section>
  );
}
