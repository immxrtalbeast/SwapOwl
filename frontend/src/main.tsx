import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AuthGate } from "./auth/AuthGate";
import { AuthProvider } from "./auth/AuthContext";
import { OrdersPage } from "./pages/orders/OrdersPage";
import { StopListPage } from "./pages/stop-list/StopListPage";
import { StockPage } from "./pages/stock/StockPage";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <AuthProvider>
      <AuthGate>
        <StockPage />
        <main style={{ maxWidth: 960, margin: "0 auto", padding: 16, fontFamily: "system-ui, sans-serif" }}>
          <StopListPage />
          <OrdersPage />
        </main>
      </AuthGate>
    </AuthProvider>
  </StrictMode>,
);
