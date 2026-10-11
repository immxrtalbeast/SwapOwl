import { useCallback, useState } from "react";
import { RoleGate } from "../../auth/AuthContext";
import { api } from "../../api/client";
import { useLive } from "../../app/useLive";
import type { Dish, Order } from "../../api/types";
import { OrderForm, MissingList } from "./OrderForm";

type State =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; dishes: Dish[]; orders: Order[] };

export function OrdersPage() {
  const [state, setState] = useState<State>({ kind: "loading" });

  const load = useCallback(async () => {
    try {
      const [dishes, orders] = await Promise.all([api.dishes(), api.orders()]);
      setState({ kind: "ready", dishes, orders });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить заказы" });
    }
  }, []);

  useLive(load, ["order.accepted", "order.rejected", "stock.changed", "stand.reset"]);

  if (state.kind === "loading") return <p role="status">Загрузка…</p>;
  if (state.kind === "error") {
    return (
      <div role="alert">
        <p>Ошибка: {state.message}</p>
        <button onClick={() => { setState({ kind: "loading" }); void load(); }}>Повторить</button>
      </div>
    );
  }
  const dishName = (id: string) => state.dishes.find((d) => d.id === id)?.name ?? id;
  const accepted = state.orders.filter((o) => o.status === "accepted");
  const rejected = state.orders.filter((o) => o.status === "rejected");

  return (
    <section>
      <h1>Заказы</h1>
      <RoleGate min="operator">
        <OrderForm dishes={state.dishes} onPlaced={load} />
      </RoleGate>
      <h2>Принятые ({accepted.length})</h2>
      {accepted.length === 0 ? <p>Принятых заказов пока нет.</p> : <OrderList orders={accepted} dishName={dishName} />}
      <h2>Отклонённые ({rejected.length})</h2>
      {rejected.length === 0 ? <p>Отклонённых заказов нет.</p> : <OrderList orders={rejected} dishName={dishName} />}
    </section>
  );
}

function OrderList({ orders, dishName }: { orders: Order[]; dishName: (id: string) => string }) {
  return (
    <ul style={{ paddingLeft: 16 }}>
      {orders.map((o) => (
        <li key={o.id} style={{ marginBottom: 8 }}>
          <time dateTime={o.at}>{new Date(o.at).toLocaleString("ru-RU")}</time>:{" "}
          {o.items.map((it) => `${dishName(it.dish_id)} × ${it.portions}`).join(", ")}
          {o.status === "rejected" && (
            <>
              <br />
              <strong>Причина отказа: нехватка продуктов</strong>
              <MissingList order={o} />
            </>
          )}
        </li>
      ))}
    </ul>
  );
}
