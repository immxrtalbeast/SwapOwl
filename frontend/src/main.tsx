/// <reference types="vite/client" />
import "@fontsource/roboto/cyrillic-300.css";
import "@fontsource/roboto/latin-300.css";
import "@fontsource/roboto/cyrillic-400.css";
import "@fontsource/roboto/latin-400.css";
import "@fontsource/roboto/cyrillic-500.css";
import "@fontsource/roboto/latin-500.css";
import "@fontsource/roboto/cyrillic-700.css";
import "@fontsource/roboto/latin-700.css";
import "@fontsource/roboto-mono/cyrillic-500.css";
import "@fontsource/roboto-mono/latin-500.css";
import "@fontsource/roboto-mono/cyrillic-700.css";
import "@fontsource/roboto-mono/latin-700.css";
import "@fontsource/neucha/cyrillic-400.css";
import "@fontsource/neucha/latin-400.css";
import "@mantine/core/styles.css";
import "@mantine/notifications/styles.css";
import "./theme/global.css";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { Button, MantineProvider } from "@mantine/core";
import { Notifications, notifications } from "@mantine/notifications";
import { AppShell } from "./app/AppShell";
import { AuthGate } from "./auth/AuthGate";
import { AuthProvider } from "./auth/AuthContext";
import { LandingGate } from "./landing/LandingGate";
import { theme } from "./theme/theme";
import { cssVariablesResolver } from "./theme/tokens";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MantineProvider theme={theme} cssVariablesResolver={cssVariablesResolver} forceColorScheme="light">
      <Notifications position="bottom-right" />
      <LandingGate>
        <AuthProvider>
          <AuthGate>
            <AppShell />
          </AuthGate>
        </AuthProvider>
      </LandingGate>
    </MantineProvider>
  </StrictMode>,
);

// Service worker только в production-сборке (не мешает dev и e2e). Кэширует оболочку, не данные.
function offerUpdate(worker: ServiceWorker) {
  notifications.show({
    id: "sw-update",
    title: "Доступна новая версия",
    message: (
      <Button
        size="xs"
        mt="xs"
        onClick={() => {
          navigator.serviceWorker.addEventListener("controllerchange", () => window.location.reload(), { once: true });
          worker.postMessage({ type: "SKIP_WAITING" });
        }}
      >
        Обновить
      </Button>
    ),
    autoClose: false,
  });
}

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker
      .register("/sw.js")
      .then((reg) => {
        if (reg.waiting && navigator.serviceWorker.controller) offerUpdate(reg.waiting);
        reg.addEventListener("updatefound", () => {
          const w = reg.installing;
          w?.addEventListener("statechange", () => {
            if (w.state === "installed" && navigator.serviceWorker.controller) offerUpdate(w);
          });
        });
      })
      .catch(() => {});
  });
}
