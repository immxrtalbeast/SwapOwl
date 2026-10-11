import { useEffect, useState } from "react";
import { StockPage } from "../pages/stock/StockPage";
import { StopListPage } from "../pages/stop-list/StopListPage";
import { OrdersPage } from "../pages/orders/OrdersPage";
import { AlertsPage } from "../pages/alerts";
import { SupplyPage } from "../pages/supply";
import { HotOffersPage } from "../pages/hot-offers";
import { MoneyPage } from "../pages/money";
import { DemoPage } from "../pages/demo";
import { AnalyticsPage } from "../pages/analytics";
import { UsersPage } from "../pages/users";
import { AccountPage } from "../pages/account";
import { ReceivingPage } from "../pages/receiving";
import { OfflineBanner } from "../components/ScreenStates";
import { events } from "../api/events";
import { AppHeader } from "./AppHeader";
import { AppNav } from "./AppNav";
import { BadgesProvider } from "./BadgesContext";
import { ClockProvider } from "./ClockContext";
import { ConnectionProvider, useConnection } from "./ConnectionContext";
import { useAuth } from "../auth/AuthContext";
import { hasRole } from "../auth/roles";
import { SCREENS, screenFromHash } from "./screens";

function ScreenView({ id }: { id: string }) {
  switch (id) {
    case "stock": return <StockPage />;
    case "receiving": return <ReceivingPage />;
    case "menu": return <StopListPage />;
    case "orders": return <OrdersPage />;
    case "alerts": return <AlertsPage />;
    case "supply": return <SupplyPage />;
    case "hot-offers": return <HotOffersPage />;
    case "money": return <MoneyPage />;
    case "demo": return <DemoPage />;
    case "analytics": return <AnalyticsPage />;
    case "account": return <AccountPage />;
    default: return <UsersPage />;
  }
}

function Offline() {
  const conn = useConnection();
  if (conn !== "offline") return null;
  return (
    <OfflineBanner
      onRetry={() => {
        events.stop();
        events.start();
      }}
    />
  );
}

/** Каркас: боковая панель, белая шапка, содержимое экрана; навигация по хэшу адреса (без библиотеки маршрутизации). */
export function AppShell() {
  const [screen, setScreen] = useState(() => screenFromHash(window.location.hash));
  useEffect(() => {
    const on = () => setScreen(screenFromHash(window.location.hash));
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);

  const { state } = useAuth();
  const role = state.kind === "user" ? state.role : null;
  const visible = SCREENS.filter((s) => !s.minRole || hasRole(role, s.minRole));

  return (
    <ConnectionProvider>
      <ClockProvider>
        <BadgesProvider>
          <a href="#so-main" className="so-skip" onClick={(e) => { e.preventDefault(); document.getElementById("so-main")?.focus(); }}>К содержимому</a>
          <div style={{ display: "flex", minHeight: "100vh" }}>
            <AppNav screens={visible} current={screen} />
            <div style={{ flex: 1, minWidth: 0, display: "flex", flexDirection: "column" }}>
              <AppHeader />
              <main id="so-main" tabIndex={-1} className="so-main" style={{ flex: 1, minWidth: 0, width: "100%", boxSizing: "border-box" }}>
                <Offline />
                <ScreenView id={screen} />
              </main>
              <footer style={{ padding: "12px var(--gutter)", font: "var(--type-small)", color: "var(--text-muted)" }}>
                SwapOwl · Концепт для линейки Swap «Флагмана»
              </footer>
            </div>
          </div>
        </BadgesProvider>
      </ClockProvider>
    </ConnectionProvider>
  );
}
