import type { ReactNode } from "react";
import { LoginScreen } from "../pages/login/LoginScreen";
import { useAuth } from "./AuthContext";
import { roleLabel } from "./roles";

// Экран входа показывается только при 401. При AUTH_REQUIRED=false API отвечает как operator — входа нет.
// После входа содержимое монтируется заново (key=epoch) и перезагружает данные на том же адресе.
export function AuthGate({ children }: { children: ReactNode }) {
  const { state, epoch, logout, reload } = useAuth();
  if (state.kind === "loading") return <p role="status">Загрузка…</p>;
  if (state.kind === "anonymous") return <LoginScreen />;
  if (state.kind === "error") {
    return (
      <div role="alert">
        <p>Ошибка: {state.message}</p>
        <button onClick={() => void reload()}>Повторить</button>
      </div>
    );
  }
  return (
    <>
      <header style={{ display: "flex", justifyContent: "flex-end", gap: 12, padding: "8px 16px", fontFamily: "system-ui, sans-serif" }}>
        <span>
          {state.login ?? "Гость"} · {roleLabel[state.role]}
        </span>
        {state.authRequired && <button onClick={() => void logout()}>Выйти</button>}
      </header>
      <div key={epoch}>{children}</div>
    </>
  );
}
