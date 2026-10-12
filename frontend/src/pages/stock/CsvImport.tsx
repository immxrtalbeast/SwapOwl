import { useRef, useState } from "react";
import type { ChangeEvent } from "react";
import { Modal, NativeSelect } from "@mantine/core";
import { IconUpload } from "@tabler/icons-react";
import { api } from "../../api/client";
import { ApiError } from "../../api/types";
import type { ProductStock } from "../../api/types";
import { PrimaryButton, SecondaryButton } from "../../components";
import { buildDelivery, decodeCsv, invoiceFromFileName, parseCsv, resolveRow } from "./csvImport";
import type { ParsedCsv } from "./csvImport";

type Status = { kind: "idle" | "sending" } | { kind: "ok" | "error"; message: string };

interface Loaded {
  idSalt: string; // номер из имени файла не уникален: добавляем дату файла, чтобы пересохранённый шаблон был новой поставкой
  createdAt: Date; // дата изменения файла: у того же файла она та же, значит и тело запроса (BodyHash включает created_at)
  parsed: ParsedCsv;
  invoice: string;
}

const cell = { padding: "6px 8px", textAlign: "left", verticalAlign: "top" } as const;
const SAMPLE_URL = "/samples/nakladnaya-primer.csv";

/** Кнопка «Загрузить накладную (CSV)» + предпросмотр таблицей и отправка одним POST /api/deliveries. */
export function CsvImport({ products, onReceived }: { products: readonly ProductStock[]; onReceived: () => void | Promise<void> }) {
  const fileInput = useRef<HTMLInputElement>(null);
  const [loaded, setLoaded] = useState<Loaded | null>(null);
  const [overrides, setOverrides] = useState<Record<number, string>>({}); // номер строки файла → id продукта
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  async function onFile(e: ChangeEvent<HTMLInputElement>) {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = "";
    if (!file) return;
    const parsed = parseCsv(decodeCsv(await file.arrayBuffer()));
    setOverrides({});
    setStatus({ kind: "idle" });
    setLoaded({ idSalt: parsed.invoice ? "" : String(file.lastModified), createdAt: new Date(file.lastModified), parsed, invoice: parsed.invoice || invoiceFromFileName(file.name) });
  }

  const rows = loaded ? loaded.parsed.rows.map((r) => resolveRow(r, products, overrides[r.line])) : [];
  const fileErrors = loaded ? [...loaded.parsed.errors, ...(loaded.invoice ? [] : ["Не указан номер накладной"])] : [];
  const canSend = loaded !== null && fileErrors.length === 0 && rows.length > 0 && rows.every((r) => r.item) && status.kind !== "sending";

  async function send() {
    if (!loaded || !canSend) return;
    setStatus({ kind: "sending" });
    try {
      const r = await api.receiveDelivery(buildDelivery(loaded.invoice, rows.map((x) => x.item!), loaded.createdAt, loaded.idSalt));
      if (r.replayed) setStatus({ kind: "ok", message: "Эта накладная уже принята — остатки не изменились" });
      else {
        setStatus({ kind: "ok", message: `Поставка принята: партий ${r.batches.length}` });
        await onReceived();
      }
      setLoaded(null);
    } catch (err) {
      // id — хэш содержимого, поэтому конфликт возможен только у пересохранённого файла с тем же содержимым.
      if (err instanceof ApiError && err.code === "delivery_conflict") {
        setStatus({ kind: "ok", message: "Эта накладная уже принята — остатки не изменились" });
        setLoaded(null);
        return;
      }
      setStatus({ kind: "error", message: err instanceof Error ? err.message : "Не удалось отправить поставку" });
    }
  }

  return (
    <div style={{ display: "grid", gap: 8 }}>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center" }}>
        <input ref={fileInput} type="file" accept=".csv,text/csv" onChange={onFile} style={{ display: "none" }} aria-label="Файл накладной CSV" />
        <SecondaryButton type="button" leftSection={<IconUpload size={16} aria-hidden="true" />} onClick={() => fileInput.current?.click()}>
          Загрузить накладную (CSV)
        </SecondaryButton>
        <a href={SAMPLE_URL} download="nakladnaya-primer.csv">Скачать пример</a>
      </div>
      {status.kind === "ok" && <p role="status" style={{ margin: 0, color: "var(--urgency-ok-fg)" }}>{status.message}</p>}
      {status.kind === "error" && !loaded && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>Ошибка: {status.message}</p>}

      <Modal opened={loaded !== null} onClose={() => setLoaded(null)} title={`Накладная ${loaded?.invoice ?? ""}`} size="xl">
        {fileErrors.map((m) => (
          <p key={m} role="alert" style={{ color: "var(--urgency-critical-fg)" }}>{m}</p>
        ))}
        {rows.length > 0 && (
          <div style={{ overflowX: "auto" }}>
            <table style={{ borderCollapse: "collapse", width: "100%" }}>
              <thead>
                <tr>
                  <th style={cell}>Стр.</th>
                  <th style={cell}>Продукт</th>
                  <th style={cell}>Количество</th>
                  <th style={cell}>Срок</th>
                  <th style={cell}>Проверка</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((r) => (
                  <tr key={r.line} style={{ borderTop: "1px solid var(--border)", color: r.errors.length ? "var(--urgency-critical-fg)" : undefined }}>
                    <td style={cell}>{r.line}</td>
                    <td style={cell}>
                      {r.productId && !overrides[r.line] ? (
                        r.productName
                      ) : (
                        <NativeSelect
                          aria-label={`Продукт, строка ${r.line}`}
                          data={[{ value: "", label: "Выберите продукт" }, ...products.map((p) => ({ value: p.id, label: p.name }))]}
                          value={overrides[r.line] ?? ""}
                          onChange={(e) => setOverrides((o) => ({ ...o, [r.line]: e.currentTarget.value }))}
                        />
                      )}
                    </td>
                    <td style={cell}>{r.quantityLabel}</td>
                    <td style={cell}>{r.expiryLabel}</td>
                    <td style={cell}>{r.errors.length ? r.errors.map((m) => <div key={m}>{m}</div>) : "ок"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        {status.kind === "error" && <p role="alert" style={{ color: "var(--urgency-critical-fg)" }}>Ошибка: {status.message}</p>}
        <div style={{ display: "flex", gap: 8, marginTop: 16 }}>
          <PrimaryButton type="button" disabled={!canSend} loading={status.kind === "sending"} onClick={send}>
            Принять поставку ({rows.length})
          </PrimaryButton>
          <SecondaryButton type="button" onClick={() => setLoaded(null)}>Отмена</SecondaryButton>
        </div>
      </Modal>
    </div>
  );
}
