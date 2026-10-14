import { useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { Badge, Modal, NativeSelect, SegmentedControl, TextInput } from "@mantine/core";
import { IconCamera, IconCopy } from "@tabler/icons-react";
import { api } from "../../api/client";
import { ApiError } from "../../api/types";
import type { InvoiceDraft, ProductStock } from "../../api/types";
import { PrimaryButton, SecondaryButton } from "../../components";
import { buildPhotoDelivery, claimText, discrepancies, draftRows, normQty, rowErrors, shrinkImage, unitRu } from "./photoInvoice";
import type { DraftRow } from "./photoInvoice";

type Status = { kind: "idle" | "reading" | "sending" } | { kind: "ok" | "error"; message: string; claim?: string };

const SAMPLE_URL = "/samples/nakladnaya-foto.jpg";
const cell = { padding: "6px 8px", textAlign: "left", verticalAlign: "top" } as const;

function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === "not_configured") return "Распознавание фото не настроено на этом сервере — загрузите CSV или введите позиции вручную.";
    if (err.code === "too_many_attempts") return "Лимит распознаваний за час исчерпан — попробуйте позже или загрузите CSV.";
    return err.message;
  }
  return err instanceof Error ? err.message : "Не удалось распознать накладную";
}

/** «Фото накладной»: снимок → черновик по справочнику → приёмка по строкам (принять, недовоз, брак) → POST /api/deliveries. */
export function PhotoInvoice({ products, onReceived }: { products: readonly ProductStock[]; onReceived: () => void | Promise<void> }) {
  const fileInput = useRef<HTMLInputElement>(null);
  const [draft, setDraft] = useState<InvoiceDraft | null>(null);
  const [photo, setPhoto] = useState<string | null>(null);
  const [number, setNumber] = useState("");
  const [rows, setRows] = useState<DraftRow[]>([]);
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  async function recognize(blob: Blob) {
    setStatus({ kind: "reading" });
    try {
      const img = await shrinkImage(blob);
      const d = await api.recognizeInvoice(img.base64, img.mediaType);
      setPhoto(`data:${img.mediaType};base64,${img.base64}`);
      setDraft(d);
      setNumber(d.number);
      setRows(draftRows(d));
      setStatus({ kind: "idle" });
    } catch (err) {
      setStatus({ kind: "error", message: errorText(err) });
    }
  }

  async function onFile(e: ChangeEvent<HTMLInputElement>) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = "";
    if (file) await recognize(file);
  }

  async function sample() {
    try {
      await recognize(await (await fetch(SAMPLE_URL)).blob());
    } catch (err) {
      setStatus({ kind: "error", message: errorText(err) });
    }
  }

  const close = () => { setDraft(null); setPhoto(null); };
  const update = (key: number, patch: Partial<DraftRow>) => setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const errors = rows.map((r) => rowErrors(r, products));
  const accepted = rows.filter((r) => r.decision === "accept");
  const diffs = discrepancies(rows);
  const today = new Date().toLocaleDateString("ru-RU");
  const claim = draft ? claimText({ number, supplier: draft.supplier }, diffs, today) : "";
  const canSend = draft !== null && number.trim() !== "" && accepted.length > 0 && errors.every((e) => e.length === 0) && status.kind !== "sending";

  async function send() {
    if (!draft || !canSend) return;
    setStatus({ kind: "sending" });
    try {
      const r = await api.receiveDelivery(buildPhotoDelivery(draft, number.trim(), rows));
      const tail = diffs.length ? ` Расхождений: ${diffs.length} — акт можно скопировать.` : "";
      if (r.replayed) setStatus({ kind: "ok", message: "Эта накладная уже принята — остатки не изменились.", claim });
      else {
        setStatus({ kind: "ok", message: `Поставка принята: партий ${r.batches.length}.${tail}`, claim });
        await onReceived();
      }
      close();
    } catch (err) {
      if (err instanceof ApiError && err.code === "delivery_conflict") {
        setStatus({ kind: "error", message: "Накладная с этим номером уже принята с другим составом — остатки не изменены. Исправления вносите корректировкой партий." });
        return;
      }
      setStatus({ kind: "error", message: errorText(err) });
    }
  }

  const copy = (text: string) => void navigator.clipboard?.writeText(text);

  return (
    <div style={{ display: "grid", gap: 8 }}>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center" }}>
        <input ref={fileInput} type="file" accept="image/*" capture="environment" onChange={onFile} style={{ display: "none" }} aria-label="Фото накладной" />
        <SecondaryButton type="button" leftSection={<IconCamera size={16} aria-hidden="true" />} loading={status.kind === "reading"} onClick={() => fileInput.current?.click()}>
          Фото накладной
        </SecondaryButton>
        <button type="button" onClick={() => void sample()} disabled={status.kind === "reading"} style={{ background: "none", border: 0, padding: 0, color: "var(--link, #0e8450)", textDecoration: "underline", cursor: "pointer", font: "inherit" }}>
          Пример фото
        </button>
      </div>
      {status.kind === "reading" && <p role="status" style={{ margin: 0 }}>Читаю накладную…</p>}
      {status.kind === "ok" && (
        <p role="status" style={{ margin: 0, color: "var(--urgency-ok-fg)" }}>
          {status.message}{" "}
          {status.claim && <SecondaryButton size="xs" leftSection={<IconCopy size={14} aria-hidden="true" />} onClick={() => copy(status.claim!)}>Скопировать акт</SecondaryButton>}
        </p>
      )}
      {status.kind === "error" && !draft && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>Ошибка: {status.message}</p>}

      <Modal opened={draft !== null} onClose={close} title="Приёмка по фото накладной" size="xl">
        {draft && (
          <div style={{ display: "grid", gap: 12 }}>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 16, alignItems: "flex-end" }}>
              {photo && <a href={photo} target="_blank" rel="noreferrer"><img src={photo} alt="Снимок накладной" style={{ height: 96, borderRadius: 4, border: "1px solid var(--border)" }} /></a>}
              <TextInput label="Номер накладной" required value={number} onChange={(e) => setNumber(e.currentTarget.value)} style={{ width: 180 }} />
              <div style={{ display: "grid", gap: 4 }}>
                {draft.supplier && <span>{draft.supplier}</span>}
                {draft.date && <span style={{ color: "var(--text-muted)" }}>Дата документа {draft.date.split("-").reverse().join(".")} (не срок годности)</span>}
                {draft.demo ? <Badge color="gray" variant="light">демо-распознавание: образец без модели</Badge> : <span style={{ color: "var(--text-muted)", fontSize: 12 }}>Распознано: {draft.model}</span>}
              </div>
            </div>
            <div style={{ overflowX: "auto" }}>
              <table style={{ borderCollapse: "collapse", width: "100%" }}>
                <thead>
                  <tr>
                    <th style={cell}>В накладной</th>
                    <th style={cell}>Продукт</th>
                    <th style={cell}>По накладной</th>
                    <th style={cell}>Принято</th>
                    <th style={cell}>Срок</th>
                    <th style={cell}>Решение</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((r, i) => {
                    const short = r.decision === "accept" && normQty(r.accepted) && normQty(r.line.quantity) && Number(normQty(r.accepted)) < Number(normQty(r.line.quantity));
                    return (
                      <tr key={r.key} style={{ borderTop: "1px solid var(--border)", opacity: r.decision === "reject" ? 0.6 : 1 }}>
                        <td style={cell}>
                          <div>{r.line.raw_name}</div>
                          {r.line.confidence === "low" && <Badge color="yellow" variant="light" size="sm" title={r.line.note}>проверьте{r.line.note ? `: ${r.line.note}` : ""}</Badge>}
                          {errors[i].map((m) => <div key={m} role="alert" style={{ color: "var(--urgency-critical-fg)", fontSize: 13 }}>{m}</div>)}
                        </td>
                        <td style={cell}>
                          <NativeSelect
                            aria-label={`Продукт: ${r.line.raw_name}`}
                            data={[{ value: "", label: "Выберите продукт" }, ...products.map((p) => ({ value: p.id, label: p.name }))]}
                            value={r.productId}
                            disabled={r.decision === "reject"}
                            onChange={(e) => update(r.key, { productId: e.currentTarget.value })}
                          />
                        </td>
                        <td style={cell}>{r.line.quantity.replace(".", ",")} {unitRu(r.line.unit)}</td>
                        <td style={cell}>
                          <TextInput
                            aria-label={`Принято: ${r.line.raw_name}, ${unitRu(r.line.unit)}`}
                            value={r.accepted}
                            disabled={r.decision === "reject"}
                            onChange={(e) => update(r.key, { accepted: e.currentTarget.value })}
                            rightSection={<span style={{ fontSize: 12 }}>{unitRu(r.line.unit)}</span>}
                            style={{ width: 110 }}
                          />
                          {short && <Badge color="orange" variant="light" size="sm">недовоз</Badge>}
                        </td>
                        <td style={cell}>
                          {r.line.expires_on ? `до ${r.line.expires_on.split("-").reverse().join(".")}` : r.line.shelf_life_hours ? `${r.line.shelf_life_hours} ч` : <span title="Срок возьмётся из карточки продукта и попросит подтверждения в «Приёмке»">из карточки</span>}
                        </td>
                        <td style={cell}>
                          <SegmentedControl
                            size="xs"
                            aria-label={`Решение: ${r.line.raw_name}`}
                            data={[{ value: "accept", label: "Принять" }, { value: "reject", label: "Брак" }]}
                            value={r.decision}
                            onChange={(v) => update(r.key, { decision: v as DraftRow["decision"] })}
                          />
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
            {diffs.length > 0 && (
              <div style={{ display: "grid", gap: 6, padding: 12, border: "1px solid var(--border)", borderRadius: 6 }}>
                <strong>Расхождения: {diffs.length}</strong>
                <ul style={{ margin: 0, paddingLeft: 20 }}>
                  {diffs.map((x) => <li key={x.name + x.kind}>{x.name} — {x.kind} {x.qty.replace(".", ",")} {unitRu(x.unit)}</li>)}
                </ul>
                <span style={{ color: "var(--text-muted)", fontSize: 13 }}>На склад попадёт только принятое. Брак и недовоз — в акт для поставщика.</span>
                <div><SecondaryButton size="xs" leftSection={<IconCopy size={14} aria-hidden="true" />} onClick={() => copy(claim)}>Скопировать акт расхождений</SecondaryButton></div>
              </div>
            )}
            {status.kind === "error" && <p role="alert" style={{ color: "var(--urgency-critical-fg)", margin: 0 }}>Ошибка: {status.message}</p>}
            <div style={{ display: "flex", gap: 8 }}>
              <PrimaryButton type="button" disabled={!canSend} loading={status.kind === "sending"} onClick={() => void send()}>
                Принять поставку ({accepted.length} из {rows.length})
              </PrimaryButton>
              <SecondaryButton type="button" onClick={close}>Отмена</SecondaryButton>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
