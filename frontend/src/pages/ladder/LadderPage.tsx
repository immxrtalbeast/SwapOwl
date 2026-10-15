import { useCallback, useState } from "react";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { LadderBatch } from "../../api/types";
import { Card, EmptyState, ErrorState, LoadingState, ScreenTitle } from "../../components";
import { formatQty } from "../stock/format";
import { formatRub } from "../money/format";
import { ladderSummary, orderedSteps, stepLink, stepValue } from "./model";

type State = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; batches: LadderBatch[] };

/** «Лестница спасения партии»: для каждой партии под риском — ступени (продать, скидка, заготовка, передать) в рублях. */
export function LadderPage() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const load = useCallback(async () => {
    try {
      setState({ kind: "ready", batches: await api.rescueLadder() });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить" });
    }
  }, []);
  useLive(load, ["stock.changed", "hotoffers.changed", "stoplist.changed", "clock.changed", "stand.reset"]);

  return (
    <section aria-labelledby="ladder-title">
      <ScreenTitle>Лестница спасения</ScreenTitle>
      <p style={{ margin: "0 0 16px", color: "var(--text-muted)", maxWidth: 720 }}>
        Для каждой партии под риском — что с ней сделать и сколько это не даст списать. Сначала продать горячим, остаток — со скидкой гостю.
        Заготовка и передача в соседнюю точку показаны ступенями, но в этой версии без расчёта.
      </p>
      {state.kind === "loading" && <LoadingState label="Считаем ступени" />}
      {state.kind === "error" && <ErrorState what="лестницу спасения" reason={state.message} onRetry={() => void load()} />}
      {state.kind === "ready" && state.batches.length === 0 && (
        <EmptyState title="Спасать нечего" text="Партий, срок которых истекает, сейчас нет." />
      )}
      {state.kind === "ready" && state.batches.length > 0 && (
        <div style={{ display: "grid", gap: 16 }}>
          {state.batches.map((b) => (
            <Card key={b.batch_id} title={`${b.product_name} — партия №${b.batch_id}`}>
              <p data-testid={`ladder-risk-${b.batch_id}`} style={{ margin: "0 0 12px", font: "var(--type-body)" }}>
                Под риском {formatQty(b.at_risk)}{b.at_risk_minor > 0 && <> · ≈ {formatRub(b.at_risk_minor)}</>} · до{" "}
                {new Date(b.expires_at).toLocaleString("ru-RU", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}
              </p>
              <ol style={{ listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 8 }}>
                {orderedSteps(b).map((s, i) => {
                  const link = stepLink(s.kind, b.product_id);
                  const best = b.best === s.kind;
                  return (
                    <li
                      key={s.kind}
                      data-testid={`ladder-${b.batch_id}-${s.kind}`}
                      data-best={best ? "true" : undefined}
                      style={{
                        display: "grid", gridTemplateColumns: "28px 1fr auto", gap: 12, alignItems: "center",
                        padding: "10px 12px", borderRadius: "var(--radius-control)", border: "1px solid var(--border)",
                        background: best ? "var(--brand-tint)" : "var(--surface)",
                      }}
                    >
                      <span aria-hidden="true" style={{ font: "var(--type-num)", color: "var(--text-muted)" }}>{i + 1}</span>
                      <span style={{ display: "grid", gap: 2, minWidth: 0 }}>
                        <strong style={{ font: "var(--type-body-strong)" }}>
                          {s.title}{best && <span style={{ marginLeft: 8, color: "var(--primary)" }}>лучшее</span>}
                        </strong>
                        <span style={{ font: "var(--type-small)", color: "var(--text-muted)" }}>
                          {s.detail}
                          {s.status === "" || s.status === undefined ? <> · спасёт {formatRub(s.saved_minor)}{s.cost_minor > 0 && <>, скидка стоит {formatRub(s.cost_minor)}</>}</> : null}
                        </span>
                      </span>
                      <span style={{ display: "grid", justifyItems: "end", gap: 4 }}>
                        <span style={{ font: "var(--type-num)", whiteSpace: "nowrap" }}>{stepValue(s)}</span>
                        {link && (
                          <a href={link.href} style={{ font: "var(--type-small)", color: "var(--primary)", textDecoration: "underline" }}>{link.label}</a>
                        )}
                      </span>
                    </li>
                  );
                })}
              </ol>
              <p data-testid={`ladder-sum-${b.batch_id}`} style={{ margin: "12px 0 0", font: "var(--type-h3)" }}>{ladderSummary(b)}</p>
            </Card>
          ))}
        </div>
      )}
    </section>
  );
}
