import type { UnitPrices } from "../../api/today";
import type { HotOffer } from "../../api/types";
import { formatRub } from "../money/format";
import { formatQty } from "../stock/format";
import { ACTION_LABELS, actionSavedMinor, realisticTarget, SAVE_SHARE } from "./realistic";
import type { LeftoverAction } from "./realistic";
import { portionsWord } from "./view";

/** «Реально продать N» и остаток с тремя подсказками. Только подсказки: в API ничего не пишется. */
/** compact — без крупной строки «реально продать N»: её уже показывает строка вокруг (экран «Сегодня»). */
export function RealisticBlock({ offer, prices, compact }: { offer: HotOffer; prices: UnitPrices; compact?: boolean }) {
  const r = realisticTarget(offer);
  if (!r) return null;
  return (
    <div data-testid="realistic" style={{ display: "grid", gap: 6, width: "100%" }}>
      {!compact && (
        <p style={{ margin: 0, font: "var(--type-h2)", color: "var(--text)" }}>
          реально продать <span style={{ font: "700 26px/30px var(--font-display)", color: "var(--primary)" }}>{r.real}</span>{" "}
          {portionsWord(r.real)}
        </p>
      )}
      {r.leftover && r.leftover.qty.qty > 0 && (
        <>
          <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>
            Останется {formatQty(r.leftover.qty)} · {r.leftover.name.toLowerCase()} — куда деть:
          </p>
          <div style={{ display: "grid", border: "1px solid var(--border)", borderRadius: 8, overflow: "hidden" }}>
            {(Object.keys(SAVE_SHARE) as LeftoverAction[]).map((a, i) => {
              const saved = actionSavedMinor(r, a, prices);
              return (
                <span
                  key={a}
                  data-testid="leftover-action"
                  style={{ display: "flex", justifyContent: "space-between", gap: 12, font: "var(--type-small)", padding: "6px 10px", borderTop: i > 0 ? "1px solid var(--border-cell)" : undefined }}
                >
                  <span>{ACTION_LABELS[a]}</span>
                  {saved !== null && <span className="so-num" style={{ whiteSpace: "nowrap", fontWeight: 600 }}>спасёт ≈ {formatRub(saved)}</span>}
                </span>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}
