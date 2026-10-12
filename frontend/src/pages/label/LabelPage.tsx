import { useEffect, useMemo, useState } from "react";
import { api } from "../../api/client";
import type { Batch, ProductStock } from "../../api/types";
import { useAuth } from "../../auth/AuthContext";
import { useStandFormat } from "../../app/useStandFormat";
import { ErrorState, LoadingState, ScreenTitle } from "../../components";
import { PrimaryButton, SecondaryButton } from "../../components";
import { qrSvg } from "../../lib/qr";
import { buildLabel, needsOpenFirst, type LabelData } from "./labelData";

type Mode = "thermal" | "a4";

const PRINT_CSS = `
.label-sheet { display: grid; }
.label-card { box-sizing: border-box; background: #fff; color: #000; border: 1px solid #000; padding: 2mm; display: grid; grid-template-columns: 1fr 18mm; gap: 1.5mm; font-family: Roboto, Arial, sans-serif; line-height: 1.15; overflow: hidden; }
.label-card .l-name { font-size: 10pt; font-weight: 700; }
.label-card .l-small { font-size: 7.5pt; }
.label-card .l-until { font-size: 11pt; font-weight: 900; }
.label-card .l-qr { width: 18mm; height: 18mm; }
.label-card .l-qr svg { width: 100%; height: 100%; display: block; }
.label-thermal .label-card { width: 58mm; height: 40mm; }
.label-a4 .label-sheet { grid-template-columns: repeat(2, 90mm); grid-auto-rows: 60mm; gap: 4mm; }
.label-a4 .label-card { width: 90mm; height: 60mm; grid-template-columns: 1fr 28mm; padding: 4mm; }
.label-a4 .l-name { font-size: 15pt; } .label-a4 .l-small { font-size: 10pt; } .label-a4 .l-until { font-size: 18pt; }
.label-a4 .l-qr { width: 28mm; height: 28mm; }
@media print {
  body * { visibility: hidden !important; }
  .label-print, .label-print * { visibility: visible !important; }
  .label-print { position: absolute; left: 0; top: 0; }
  .label-card { border-color: #000; }
}
`;

function Label({ d }: { d: LabelData }) {
  return (
    <div className="label-card">
      <div>
        <div className="l-name">{d.product}</div>
        <div className="l-small">вскрыто {d.openedStamp}{d.by ? ` · ${d.by}` : ""}</div>
        <div className="l-until">ГОДНО ДО {d.untilStamp}</div>
        <div className="l-small">партия №{d.batchId}</div>
      </div>
      <div className="l-qr" role="img" aria-label={`QR партии №${d.batchId}`} dangerouslySetInnerHTML={{ __html: qrSvg(d.qrUrl, 1) }} />
    </div>
  );
}

/** Этикетка «вскрыто / годно до» с QR партии: 58×40 мм (термопринтер) или лист A4 по 8 штук. */
export function LabelPage({ batchId }: { batchId: number }) {
  const f = useStandFormat();
  const { state } = useAuth();
  const [data, setData] = useState<{ batch: Batch; product: ProductStock } | "missing" | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [mode, setMode] = useState<Mode>("thermal");

  useEffect(() => {
    let live = true;
    setData(null);
    setErr(null);
    Promise.all([api.batches(), api.products()])
      .then(([bs, ps]) => {
        if (!live) return;
        const batch = bs.find((b) => b.id === batchId);
        const product = batch && ps.find((p) => p.id === batch.product_id);
        setData(batch && product ? { batch, product } : "missing");
      })
      .catch((e) => live && setErr(e instanceof Error ? e.message : "Не удалось загрузить партию"));
    return () => { live = false; };
  }, [batchId]);

  const login = state.kind === "user" ? state.login : null;
  const nowMs = f.now.getTime();
  const label = useMemo(
    () => (data && data !== "missing"
      ? buildLabel({ product: data.product.name, batchId, expiresAt: data.batch.expires_at, openedAt: data.batch.opened_at, nowMs, timeZone: f.timezone, login, origin: window.location.origin })
      : null),
    [data, batchId, nowMs, f.timezone, login],
  );

  if (err) return <ErrorState what="этикетку" reason={err} onRetry={() => window.location.reload()} />;
  if (data === null) return <LoadingState label="Загрузка этикетки" />;
  if (data !== "missing" && needsOpenFirst(data.batch.opened_at, data.product.shelf_after_open_hours)) {
    return <ErrorState what="этикетку" reason="Сначала нажмите «Вскрыл упаковку» на складе: срок вскрытой части назначает сервер" onRetry={() => { window.location.hash = "#/stock"; }} />;
  }
  if (data === "missing" || !label) return <ErrorState what="партию" reason={`Партия №${batchId} не найдена`} onRetry={() => { window.location.hash = "#/stock"; }} />;

  const count = mode === "a4" ? 8 : 1;
  return (
    <section aria-label="Этикетка" className={`label-${mode}`}>
      <style>{PRINT_CSS}{`@media print { @page { size: ${mode === "a4" ? "A4" : "58mm 40mm"}; margin: ${mode === "a4" ? "10mm" : "0"}; } }`}</style>
      <ScreenTitle>Этикетка партии №{batchId}</ScreenTitle>
      <div style={{ display: "flex", gap: 8, margin: "12px 0", flexWrap: "wrap" }}>
        <SecondaryButton aria-pressed={mode === "thermal"} onClick={() => setMode("thermal")}>Термопринтер 58×40</SecondaryButton>
        <SecondaryButton aria-pressed={mode === "a4"} onClick={() => setMode("a4")}>Лист A4 · 8 штук</SecondaryButton>
        <PrimaryButton onClick={() => window.print()}>Печать</PrimaryButton>
        <SecondaryButton onClick={() => { window.location.hash = "#/stock"; }}>На склад</SecondaryButton>
      </div>
      <div className="label-print">
        <div className="label-sheet">
          {Array.from({ length: count }, (_, i) => <Label key={i} d={label} />)}
        </div>
      </div>
    </section>
  );
}
