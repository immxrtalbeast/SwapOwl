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
import { MantineProvider } from "@mantine/core";
import { Notifications } from "@mantine/notifications";
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
