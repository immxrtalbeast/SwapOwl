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
import { AppHeader } from "./AppHeader";
import { ClockProvider } from "./ClockContext";
import { ConnectionProvider } from "./ConnectionContext";
import { useAuth } from "../auth/AuthContext";
import { hasRole } from "../auth/roles";
import { SCREENS, screenFromHash } from "./screens";

function ScreenView({ id }: { id: string }) {
  switch (id) {
    case "stock": return <StockPage />;
    case "menu": return <StopListPage />;
    case "orders": return <OrdersPage />;
    case "alerts": return <AlertsPage />;
    case "supply": return <SupplyPage />;
    case "hot-offers": return <HotOffersPage />;
    case "money": return <MoneyPage />;
    case "demo": return <DemoPage />;
    case "analytics": return <AnalyticsPage />;
    default: return <UsersPage />;
  }
}

/** Каркас: шапка + навигация по хэшу адреса (без библиотеки маршрутизации). */
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
        <AppHeader />
        <div style={{ display: "flex", flexWrap: "wrap", fontFamily: "system-ui, sans-serif" }}>
          <nav aria-label="Экраны" style={{ minWidth: 200, padding: 16 }}>
            <ul style={{ listStyle: "none", margin: 0, padding: 0 }}>
              {visible.map((s) => (
                <li key={s.id} style={{ marginBottom: 4 }}>
                  <a
                    href={`#/${s.id}`}
                    aria-current={s.id === screen ? "page" : undefined}
                    style={{ fontWeight: s.id === screen ? 700 : 400 }}
                  >
                    {s.title}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
          <main style={{ flex: 1, minWidth: 0, maxWidth: 960, padding: 16 }}>
            <ScreenView id={screen} />
          </main>
        </div>
      </ClockProvider>
    </ConnectionProvider>
  );
}
