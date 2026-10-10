import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { StockPage } from "./pages/stock/StockPage";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <StockPage />
  </StrictMode>,
);
