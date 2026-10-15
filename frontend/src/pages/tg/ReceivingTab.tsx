import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { ProductStock } from "../../api/types";
import { ScanPanel } from "../receiving/ScanPanel";
import { PhotoInvoice } from "../stock/PhotoInvoice";
import { haptic } from "./sdk";
import { errorText, heading, muted, section } from "./ui";

/** «Приёмка»: скан упаковки (GS1/DataMatrix) и фото накладной — камера телефона. */
export function ReceivingTab() {
  const [products, setProducts] = useState<ProductStock[]>([]);
  const [error, setError] = useState<string | null>(null);
  const load = async () => {
    try {
      setProducts(await api.products());
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Не удалось загрузить справочник продуктов");
      haptic("error");
    }
  };
  useEffect(() => {
    void load();
  }, []);
  return (
    <div style={section}>
      <h2 style={heading}>Приёмка поставки</h2>
      <p style={muted}>Наведите камеру на код упаковки или сфотографируйте накладную.</p>
      {error && <p role="alert" style={errorText}>{error}</p>}
      <ScanPanel onSent={async () => { haptic("success"); await load(); }} />
      <PhotoInvoice products={products} onReceived={async () => { haptic("success"); await load(); }} />
    </div>
  );
}
