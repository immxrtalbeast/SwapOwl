import { useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type { ProductStock } from "../../api/types";
import { Card, PrimaryButton, SecondaryButton } from "../../components";
import { SAMPLE_MILK, parseGs1 } from "../../lib/gs1";
import { isLineReady, loadGtinMap, lineFromScan, rememberGtin, toDeliveryItem, type ScanLine } from "./scan";

/** Камера: BarcodeDetector, где он есть, иначе лениво подгружаемый @zxing/browser. */
async function startCamera(video: HTMLVideoElement, onCode: (s: string) => void): Promise<() => void> {
  const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } });
  video.srcObject = stream;
  await video.play();
  let stop = false;
  const BD = (window as unknown as { BarcodeDetector?: new (o: object) => { detect(v: HTMLVideoElement): Promise<{ rawValue: string }[]> } }).BarcodeDetector;
  if (BD) {
    const det = new BD({ formats: ["data_matrix", "code_128", "qr_code"] });
    const tick = async () => {
      if (stop) return;
      try {
        const r = await det.detect(video);
        if (r[0]) onCode(r[0].rawValue);
      } catch {
        /* кадр не готов */
      }
      setTimeout(tick, 300);
    };
    void tick();
    return () => {
      stop = true;
      stream.getTracks().forEach((t) => t.stop());
    };
  }
  const { BrowserMultiFormatReader } = await import("@zxing/browser");
  const reader = new BrowserMultiFormatReader();
  const controls = await reader.decodeFromStream(stream, video, (res) => {
    if (res) onCode(res.getText());
  });
  return () => {
    controls.stop();
    stream.getTracks().forEach((t) => t.stop());
  };
}

export function ScanPanel({ onSent }: { onSent: () => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [products, setProducts] = useState<ProductStock[]>([]);
  const [lines, setLines] = useState<ScanLine[]>([]);
  const [pending, setPending] = useState<ReturnType<typeof parseGs1>>(null);
  const [manual, setManual] = useState("");
  const [pick, setPick] = useState("");
  const [msg, setMsg] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const seq = useRef(0);
  // id и номер живут до успешной отправки: повтор после потерянного ответа не задвоит поставку.
  const [delivery, setDelivery] = useState(() => ({ id: crypto.randomUUID(), number: `SCAN-${Date.now()}` }));
  const last = useRef("");

  useEffect(() => {
    if (open) api.products().then(setProducts).catch(() => setMsg("Не удалось загрузить справочник продуктов"));
  }, [open]);

  function accept(raw: string) {
    if (raw === last.current) return;
    last.current = raw;
    const g = parseGs1(raw);
    if (!g || !g.gtin) {
      setMsg("Код не распознан как GS1");
      return;
    }
    setMsg(null);
    const pid = loadGtinMap()[g.gtin];
    const p = products.find((x) => x.id === pid);
    if (p) setLines((ls) => [...ls, lineFromScan(g, p, ++seq.current)]);
    else setPending(g);
  }

  const acceptRef = useRef(accept);
  acceptRef.current = accept;

  async function openCamera() {
    setOpen(true);
    setMsg(null);
  }

  useEffect(() => {
    if (!open || !video.current || !navigator.mediaDevices?.getUserMedia) return;
    let stop: (() => void) | undefined;
    let dead = false;
    startCamera(video.current, (s) => acceptRef.current(s))
      .then((st) => (dead ? st() : (stop = st)))
      .catch(() => setMsg("Камера недоступна — вставьте код вручную"));
    return () => {
      dead = true;
      stop?.();
    };
  }, [open]);

  function bind() {
    const p = products.find((x) => x.id === pick);
    if (!pending?.gtin || !p) return;
    rememberGtin(pending.gtin, p.id);
    setLines((ls) => [...ls, lineFromScan(pending, p, ++seq.current)]);
    setPending(null);
    setPick("");
    last.current = "";
  }

  async function send() {
    setBusy(true);
    try {
      await api.receiveDelivery({
        id: delivery.id,
        number: delivery.number,
        created_at: new Date().toISOString(),
        items: lines.map((l) => toDeliveryItem(l, products)),
      });
      setLines([]);
      last.current = "";
      setDelivery({ id: crypto.randomUUID(), number: `SCAN-${Date.now()}` });
      await onSent();
    } catch (e) {
      setMsg(e instanceof Error ? e.message : "Не удалось отправить накладную");
    } finally {
      setBusy(false);
    }
  }

  const name = (id: string) => products.find((p) => p.id === id)?.name ?? id;

  if (!open)
    return (
      <div style={{ marginBottom: 16 }}>
        <PrimaryButton style={{ height: 48 }} onClick={() => void openCamera()}>
          Сканировать упаковку
        </PrimaryButton>
      </div>
    );

  return (
    <Card>
      <div style={{ display: "grid", gap: 12 }}>
        <video ref={video} playsInline muted style={{ width: "100%", maxHeight: 240, background: "#000" }} />
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          <input
            aria-label="Код вручную"
            placeholder="Вставить код вручную"
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            style={{ flex: 1, minWidth: 200, height: 48, padding: "0 12px" }}
          />
          <SecondaryButton style={{ height: 48 }} onClick={() => { last.current = ""; accept(manual); }}>
            Добавить
          </SecondaryButton>
          <SecondaryButton style={{ height: 48 }} onClick={() => { last.current = ""; accept(SAMPLE_MILK); }}>
            Пример: молоко
          </SecondaryButton>
          <SecondaryButton style={{ height: 48 }} onClick={() => setOpen(false)}>
            Закрыть
          </SecondaryButton>
        </div>
        {msg && <p role="alert" style={{ margin: 0 }}>{msg}</p>}
        {pending && (
          <div>
            GTIN {pending.gtin} не найден в справочнике. Выберите продукт (запомним на этом устройстве):
            <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
              <select aria-label="Продукт" value={pick} onChange={(e) => setPick(e.target.value)} style={{ height: 48, flex: 1 }}>
                <option value="">—</option>
                {products.map((p) => (
                  <option key={p.id} value={p.id}>{p.name}</option>
                ))}
              </select>
              <PrimaryButton style={{ height: 48 }} disabled={!pick} onClick={bind}>Запомнить</PrimaryButton>
            </div>
          </div>
        )}
        {lines.length > 0 && (
          <>
            <ul style={{ margin: 0, padding: 0, listStyle: "none" }}>
              {lines.map((l) => (
                <li key={l.key} data-testid="scan-line">
                  {name(l.product_id)} ·{" "}
                  <input
                    aria-label={`Количество, ${l.unit}`}
                    inputMode="decimal"
                    placeholder="укажите количество"
                    value={l.quantity}
                    onChange={(e) => setLines((ls) => ls.map((x) => (x.key === l.key ? { ...x, quantity: e.target.value } : x)))}
                    style={{ width: 120, height: 36, padding: "0 8px" }}
                  />{" "}
                  {l.unit} · {l.expires_on ? `годен до ${l.expires_on}` : "срок не указан"}
                  {!isLineReady(l) && <span role="note"> — укажите количество</span>}
                </li>
              ))}
            </ul>
            <PrimaryButton style={{ height: 48 }} disabled={busy || !lines.every(isLineReady)} onClick={() => void send()}>
              Принять поставку ({lines.length})
            </PrimaryButton>
          </>
        )}
      </div>
    </Card>
  );
}
