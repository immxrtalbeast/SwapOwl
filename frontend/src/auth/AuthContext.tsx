import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { api, setUnauthorizedHandler } from "../api/client";
import { hasRole } from "./roles";
import type { Role } from "./roles";

export type AuthState =
  | { kind: "loading" }
  | { kind: "anonymous" } // нужен вход: сессии нет или она истекла (401)
  | { kind: "error"; message: string }
  | { kind: "user"; login: string | null; role: Role; authRequired: boolean };

interface AuthValue {
  state: AuthState;
  /** Увеличивается при каждом входе: по нему экраны перезагружают данные. */
  epoch: number;
  login: (login: string, password: string) => Promise<void>;
  telegramLogin: (initData: string) => Promise<void>;
  logout: () => Promise<void>;
  reload: () => Promise<void>;
}

const Ctx = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ kind: "loading" });
  const [epoch, setEpoch] = useState(0);

  const reload = useCallback(async () => {
    try {
      const me = await api.me();
      setState({ kind: "user", login: me.login, role: me.role, authRequired: me.auth_required });
    } catch (e) {
      // 401 уже переводит в anonymous через обработчик клиента.
      if (e instanceof Error && "status" in e && (e as { status: number }).status === 401) return;
      setState({ kind: "error", message: e instanceof Error ? e.message : "Не удалось получить сведения о пользователе" });
    }
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => setState({ kind: "anonymous" }));
    void reload();
    return () => setUnauthorizedHandler(null);
  }, [reload]);

  const login = useCallback(async (l: string, p: string) => {
    await api.login(l, p);
    await reload();
    setEpoch((n) => n + 1);
  }, [reload]);

  const telegramLogin = useCallback(async (initData: string) => {
    await api.telegramLogin(initData);
    await reload();
    setEpoch((n) => n + 1);
  }, [reload]);

  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setState({ kind: "anonymous" });
    }
  }, []);

  const value = useMemo(() => ({ state, epoch, login, telegramLogin, logout, reload }), [state, epoch, login, telegramLogin, logout, reload]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useAuth(): AuthValue {
  const v = useContext(Ctx);
  if (!v) throw new Error("useAuth вне AuthProvider");
  return v;
}

/** Действия, недоступные роли, скрываются (frontend-prompt.md, RoleGate). */
export function RoleGate({ min, children }: { min: Role; children: ReactNode }) {
  const { state } = useAuth();
  const role = state.kind === "user" ? state.role : null;
  return hasRole(role, min) ? <>{children}</> : null;
}
