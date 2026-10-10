import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { OrdersPage } from "./pages/orders/OrdersPage";
import { StockPage } from "./pages/stock/StockPage";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <StockPage />
    <main style={{ maxWidth: 960, margin: "0 auto", padding: 16, fontFamily: "system-ui, sans-serif" }}>
      <OrdersPage />
    </main>
  </StrictMode>,
);
