import type { UnitPrices } from "../../api/today";
import type { HotOffer } from "../../api/types";
import { formatRub } from "../money/format";
import { formatQty } from "../stock/format";
import { ACTION_LABELS, actionSavedMinor, realisticTarget, SAVE_SHARE } from "./realistic";
import type { LeftoverAction } from "./realistic";
import { portionsWord } from "./view";

/** «Реально продать N» и остаток с тремя подсказками. Только подсказки: в API ничего не пишется. */
export function RealisticBlock({ offer, prices }: { offer: HotOffer; prices: UnitPrices }) {
  const r = realisticTarget(offer);
  if (!r) return null;
  return (
    <div data-testid="realistic" style={{ display: "grid", gap: 6 }}>
      <p style={{ margin: 0, font: "var(--type-kpi)", fontSize: 28, lineHeight: 1.1, color: "var(--primary)" }}>
        реально продать {r.real}{" "}
        <span style={{ font: "var(--type-label)", color: "var(--text)" }}>{portionsWord(r.real)}</span>
      </p>
      {r.leftover && r.leftover.qty.qty > 0 && (
        <>
          <p style={{ margin: 0, font: "var(--type-body)" }}>
            остаток {formatQty(r.leftover.qty)} {r.leftover.name} — что сделать:
          </p>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
            {(Object.keys(SAVE_SHARE) as LeftoverAction[]).map((a) => {
              const saved = actionSavedMinor(r, a, prices);
              return (
                <span key={a} data-testid="leftover-action" style={{ font: "var(--type-small)", border: "1px solid var(--border)", borderRadius: 8, padding: "4px 8px" }}>
                  {ACTION_LABELS[a]}
                  {saved !== null && <> · спасёт ≈ {formatRub(saved)}</>}
                </span>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
