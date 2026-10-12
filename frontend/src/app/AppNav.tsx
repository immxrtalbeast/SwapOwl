import {
  IconBell, IconBoxSeam, IconBuildingStore, IconChartBar, IconCoin, IconFlame, IconInbox, IconListCheck, IconPlayerPlay,
  IconSalad, IconTruckDelivery, IconUser, IconUsers, IconShoppingCart,
} from "@tabler/icons-react";
import { Logo } from "../components/Logo";
import { useBadges } from "./BadgesContext";
import type { Screen } from "./screens";

const ICONS: Record<string, typeof IconBell> = {
  stock: IconBoxSeam,
  receiving: IconInbox,
  menu: IconSalad,
  "hot-offers": IconFlame,
  supply: IconTruckDelivery,
  orders: IconShoppingCart,
  alerts: IconBell,
  money: IconCoin,
  analytics: IconChartBar,
  network: IconBuildingStore,
  demo: IconPlayerPlay,
  account: IconUser,
  users: IconUsers,
};

interface Props {
  screens: readonly Screen[];
  current: string;
}

/** Боковая панель на --primary: инверсный логотип, пункты с иконкой, счётчики; на < 992 px — только иконки. */
export function AppNav({ screens, current }: Props) {
  const { alerts, unconfirmed } = useBadges();
  const counts: Record<string, number | null> = {
    alerts: alerts ? alerts.expiring + alerts.doomed + alerts.expired : null,
    receiving: unconfirmed,
  };
  const item = (s: Screen) => {
    const Icon = ICONS[s.id] ?? IconListCheck;
    const active = s.id === current;
    const n = counts[s.id];
    return (
      <li key={s.id}>
        <a
          href={`#/${s.id}`}
          title={s.title}
          aria-current={active ? "page" : undefined}
          aria-label={n ? `${s.title}: ${n}` : s.title}
          style={{
            display: "flex", alignItems: "center", gap: 12, padding: "8px 12px", borderRadius: 8, textDecoration: "none",
            color: "var(--on-primary)", font: "var(--type-body-strong)",
            background: active ? "rgba(255,255,255,.18)" : "transparent",
          }}
        >
          <Icon size={20} aria-hidden="true" style={{ flexShrink: 0 }} />
          <span className="so-nav-label" style={{ flex: 1 }}>{s.title}</span>
          {n ? (
            <span className="so-num so-nav-label" style={{ background: "var(--on-primary)", color: "var(--primary)", borderRadius: 999, padding: "0 8px" }}>
              {n}
            </span>
          ) : null}
        </a>
      </li>
    );
  };
  const list = { listStyle: "none", margin: 0, padding: 0, display: "grid", gap: 4 } as const;
  return (
    <nav
      aria-label="Экраны"
      className="so-nav"
      style={{
        position: "sticky", top: 0, alignSelf: "flex-start", height: "100vh", boxSizing: "border-box", flexShrink: 0,
        display: "flex", flexDirection: "column", gap: 16, padding: 12, background: "var(--primary)", overflowY: "auto",
      }}
    >
      <a href="#/stock" style={{ padding: "4px 8px", textDecoration: "none" }} aria-label="SwapOwl — на склад">
        <Logo variant="inverse" size={36} withWord />
      </a>
      <ul style={list}>{screens.filter((s) => !s.bottom).map(item)}</ul>
      <ul style={{ ...list, marginTop: "auto" }}>{screens.filter((s) => s.bottom).map(item)}</ul>
    </nav>
  );
}
