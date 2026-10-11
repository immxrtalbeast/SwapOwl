import { useCallback, useState } from "react";
import { RoleGate } from "../../auth/AuthContext";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { Batch, ProductStock } from "../../api/types";
import { DeliveryForm } from "./DeliveryForm";
import { formatQty, formatTimeLeft } from "./format";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; products: ProductStock[]; batches: Batch[] };

export function StockPage() {
  const [state, setState] = useState<State>({ kind: "loading" });

  const load = useCallback(async () => {
    try {
      const [products, batches] = await Promise.all([api.products(), api.batches()]);
      setState({ kind: "ready", products, batches });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить склад" });
    }
  }, []);

  useLive(load, ["stock.changed", "stand.reset"]);

  return (
    <section>
      <h1>Склад</h1>
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => { setState({ kind: "loading" }); void load(); }}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && state.products.length === 0 && <p>Продуктов пока нет.</p>}
      {state.kind === "ready" &&
        state.products.map((p) => (
          <ProductRow key={p.id} product={p} batches={state.batches.filter((b) => b.product_id === p.id)} />
        ))}
      <RoleGate min="operator">
        <DeliveryForm onReceived={load} />
      </RoleGate>
    </section>
  );
}

function ProductRow({ product, batches }: { product: ProductStock; batches: Batch[] }) {
  return (
    <details style={{ borderBottom: "1px solid #ddd", padding: "8px 0" }}>
      <summary>
        <strong>{product.name}</strong> — доступно {formatQty(product.available)}, партий: {product.batches_count}
        {product.nearest_expires_at && (
          <span title={new Date(product.nearest_expires_at).toLocaleString("ru-RU")}>
            {" "}
            · ближайший срок {new Date(product.nearest_expires_at).toLocaleDateString("ru-RU")}
          </span>
        )}
      </summary>
      {batches.length === 0 ? (
        <p>Партий нет.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th scope="col">Остаток</th>
              <th scope="col">Поступила</th>
              <th scope="col">Срок</th>
              <th scope="col">Осталось</th>
            </tr>
          </thead>
          <tbody>
            {batches.map((b) => (
              <tr key={b.id} style={b.status === "expired" ? { color: "#b00020" } : undefined}>
                <td>{formatQty(b.remaining)}</td>
                <td>{new Date(b.received_at).toLocaleString("ru-RU")}</td>
                <td>{new Date(b.expires_at).toLocaleString("ru-RU")}</td>
                <td title={new Date(b.expires_at).toLocaleString("ru-RU")}>{formatTimeLeft(b.time_left_seconds)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </details>
  );
}
