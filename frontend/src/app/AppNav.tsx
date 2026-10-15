import {
  IconBell, IconBoxSeam, IconBulb, IconBuildingStore, IconChartBar, IconCoin, IconFlame, IconInbox, IconListCheck, IconPlayerPlay,
  IconSalad, IconTruckDelivery, IconUser, IconUsers, IconShoppingCart, IconSunHigh,
} from "@tabler/icons-react";
import { Logo } from "../components/Logo";
import { useBadges } from "./BadgesContext";
import type { Screen } from "./screens";

const ICONS: Record<string, typeof IconBell> = {
  today: IconSunHigh,
  stock: IconBoxSeam,
  receiving: IconInbox,
  menu: IconSalad,
  "hot-offers": IconFlame,
  "what-if": IconBulb,
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
            display: "flex", alignItems: "center", gap: 12, padding: "8px 12px", borderRadius: 8, textDecoration: "none", minWidth: 0,
            color: "var(--on-urgency-solid)", font: "var(--type-body-strong)",
            background: active ? "rgba(255,255,255,.18)" : "transparent",
          }}
        >
          <Icon size={20} aria-hidden="true" style={{ flexShrink: 0 }} />
          <span className="so-nav-label" style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{s.title}</span>
          {n ? (
            <span className="so-num so-nav-label" style={{ background: "var(--on-urgency-solid)", color: "#0b4a2d", borderRadius: 999, padding: "0 8px" }}>
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
      className="so-nav so-nav-glass"
      style={{
        position: "sticky", top: 0, alignSelf: "flex-start", height: "100vh", boxSizing: "border-box", flexShrink: 0,
        // Без горизонтальной прокрутки: длинные названия обрезаются многоточием, а не раздвигают панель.
        display: "flex", flexDirection: "column", gap: 16, padding: 12, overflowY: "auto", overflowX: "hidden",
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
