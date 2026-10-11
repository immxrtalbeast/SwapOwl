import { useCallback, useState } from "react";
import { Tabs } from "@mantine/core";
import { RoleGate } from "../../auth/AuthContext";
import { api } from "../../api/client";
import { ApiError } from "../../api/types";
import type { Dish, Order, OrdersPage as Page } from "../../api/types";
import { useLive } from "../../app/useLive";
import { useStandFormat } from "../../app/useStandFormat";
import { Card, DataTable, EmptyState, ErrorState, LoadingState, ScreenTitle, TextButton, UrgencyBadge } from "../../components";
import type { Column } from "../../components";
import { formatNumber } from "../../lib/format";
import { OrderForm } from "./OrderForm";
import { ORDER_STATES, appendPage, itemsText, rejectionReason, sectionTitle, shortfallText, writeoffLines } from "./view";

const PAGE = 50;
type Tab = Order["status"];
type State =
  | { kind: "loading" }
  | { kind: "error"; message: string; requestId?: string }
  | { kind: "ready"; dishes: Dish[]; accepted: Page; rejected: Page };

export function OrdersPage() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [tab, setTab] = useState<Tab>("accepted");
  const [more, setMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);
  const fmt = useStandFormat();

  const load = useCallback(async () => {
    try {
      const [dishes, accepted, rejected] = await Promise.all([
        api.dishes(),
        api.ordersPage("accepted", { limit: PAGE }),
        api.ordersPage("rejected", { limit: PAGE }),
      ]);
      setState({ kind: "ready", dishes, accepted, rejected });
    } catch (e) {
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось загрузить заказы" });
    }
  }, []);

  useLive(load, ["order.accepted", "order.rejected", "stock.changed", "stand.reset"]);

  async function loadMore(status: Tab, current: Page) {
    const last = current.orders[current.orders.length - 1];
    if (!last) return;
    setMore(true);
    setMoreError(null);
    try {
      const next = await api.ordersPage(status, { limit: PAGE, before: last.id });
      setState((s) =>
        s.kind === "ready"
          ? { ...s, [status]: { orders: appendPage(s[status].orders, next.orders), total: next.total } }
          : s,
      );
    } catch (e) {
      setMoreError(e instanceof ApiError || e instanceof Error ? e.message : "Не удалось загрузить ещё");
    } finally {
      setMore(false);
    }
  }

  if (state.kind === "loading") {
    return (
      <section>
        <ScreenTitle>Заказы</ScreenTitle>
        <LoadingState />
      </section>
    );
  }
  if (state.kind === "error") {
    return (
      <section>
        <ScreenTitle>Заказы</ScreenTitle>
        <ErrorState what="заказы" reason={state.message} onRetry={() => { setState({ kind: "loading" }); void load(); }} />
      </section>
    );
  }

  const dishName = (id: string) => state.dishes.find((d) => d.id === id)?.name ?? id;
  const timeCol: Column<Order> = {
    key: "at",
    header: "Время",
    cell: (o) => <time dateTime={o.at} title={fmt.exact(o.at)}>{fmt.dateTime(o.at)}</time>,
    width: 150,
  };
  const dishesCol: Column<Order> = { key: "items", header: "Блюда", cell: (o) => itemsText(o, dishName) };
  const statusCol: Column<Order> = { key: "status", header: "Статус", cell: (o) => <UrgencyBadge def={ORDER_STATES[o.status]} />, width: 130 };
  const acceptedColumns: Column<Order>[] = [
    timeCol,
    dishesCol,
    statusCol,
    {
      key: "writeoffs",
      header: "Списано по партиям",
      cell: (o) => {
        const lines = writeoffLines(o);
        const short = shortfallText(o.shortfalls);
        return (
          <div style={{ display: "grid", gap: 2, padding: "4px 0" }}>
            {lines.map((l) => (
              <span key={l} style={{ font: "var(--type-num)" }}>{l}</span>
            ))}
            {short && <span style={{ font: "var(--type-small)", color: "var(--urgency-warn-fg)" }}>{short}</span>}
            {lines.length === 0 && !short && <span style={{ color: "var(--text-subtle)" }}>—</span>}
          </div>
        );
      },
    },
  ];
  const rejectedColumns: Column<Order>[] = [
    timeCol,
    dishesCol,
    statusCol,
    { key: "reason", header: "Причина", cell: (o) => rejectionReason(o.missing) },
  ];

  const pane = (status: Tab, page: Page, columns: Column<Order>[], emptyTitle: string, emptyText: string) => (
    <Tabs.Panel value={status} pt="md">
      {page.orders.length === 0 ? (
        <EmptyState title={emptyTitle} text={emptyText} />
      ) : (
        <>
          <DataTable caption={sectionTitle(status === "accepted" ? "Принятые" : "Отклонённые", page.total)} columns={columns} rows={page.orders} rowKey={(o) => o.id} />
          <div style={{ display: "flex", alignItems: "center", gap: 12, padding: 12, font: "var(--type-small)", color: "var(--text-muted)" }}>
            <span>Показано {page.orders.length} из {formatNumber(page.total)}</span>
            {page.orders.length < page.total && (
              <TextButton loading={more} onClick={() => void loadMore(status, page)}>
                Показать ещё
              </TextButton>
            )}
            {moreError && <span role="alert" style={{ color: "var(--urgency-critical-fg)" }}>{moreError}</span>}
          </div>
        </>
      )}
    </Tabs.Panel>
  );

  return (
    <section style={{ display: "grid", gap: 24 }}>
      <ScreenTitle>Заказы</ScreenTitle>
      <RoleGate min="operator">
        <OrderForm dishes={state.dishes} onPlaced={load} />
      </RoleGate>
      <Card flush>
        <Tabs value={tab} onChange={(v) => setTab((v as Tab | null) ?? "accepted")}>
          <Tabs.List style={{ padding: "0 var(--card-pad)" }}>
            <Tabs.Tab value="accepted">{sectionTitle("Принятые", state.accepted.total)}</Tabs.Tab>
            <Tabs.Tab value="rejected">{sectionTitle("Отклонённые", state.rejected.total)}</Tabs.Tab>
          </Tabs.List>
          {pane("accepted", state.accepted, acceptedColumns, "Принятых заказов пока нет", "Оформите заказ выше — списание по партиям появится здесь.")}
          {pane("rejected", state.rejected, rejectedColumns, "Отклонённых заказов нет", "Все заказы были обеспечены продуктами.")}
        </Tabs>
      </Card>
    </section>
  );
}
