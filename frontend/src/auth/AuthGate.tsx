import type { ReactNode } from "react";
import { ErrorState, LoadingState } from "../components/ScreenStates";
import { LoginScreen } from "../pages/login/LoginScreen";
import { useAuth } from "./AuthContext";
import { TgSignIn } from "../pages/tg/TgSignIn";
import { isTgHash } from "../pages/tg/model";

// Экран входа показывается только при 401. При AUTH_REQUIRED=false API отвечает как operator — входа нет.
// После входа содержимое монтируется заново (key=epoch) и перезагружает данные на том же адресе.
// Роль и «Выйти» живут в шапке приложения (app/AppHeader).
export function AuthGate({ children }: { children: ReactNode }) {
  const { state, epoch, reload } = useAuth();
  if (state.kind === "loading") return <div style={{ padding: 24 }}><LoadingState label="Загрузка" /></div>;
  if (state.kind === "anonymous") return isTgHash(window.location.hash) ? <TgSignIn fallback={<LoginScreen />} /> : <LoginScreen />;
  if (state.kind === "error") return <ErrorState what="данные учётной записи" reason={state.message} onRetry={() => void reload()} />;
  return <div key={epoch}>{children}</div>;
}
