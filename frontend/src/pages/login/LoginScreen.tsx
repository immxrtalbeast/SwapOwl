import { useState } from "react";
import type { FormEvent } from "react";
import { PasswordInput, TextInput } from "@mantine/core";
import { Card } from "../../components/Card";
import { Logo } from "../../components/Logo";
import { PrimaryButton } from "../../components/Buttons";
import { useAuth } from "../../auth/AuthContext";
import { loginErrorMessage } from "./loginMessage";

export function LoginScreen() {
  const { login } = useAuth();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [sending, setSending] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setSending(true);
    try {
      await login(name.trim(), password);
    } catch (err) {
      setError(loginErrorMessage(err));
      setSending(false);
    }
  }

  return (
    <main
      style={{
        minHeight: "100vh", display: "grid", placeItems: "center", padding: 16, boxSizing: "border-box",
        backgroundColor: "var(--paper)",
        backgroundImage:
          "linear-gradient(var(--brand-grid) 1px, transparent 1px), linear-gradient(90deg, var(--brand-grid) 1px, transparent 1px)",
        backgroundSize: "var(--grid-step) var(--grid-step)",
      }}
    >
      <div style={{ width: "100%", maxWidth: 380, display: "grid", gap: 16 }}>
        <div style={{ display: "grid", justifyItems: "center", gap: 8 }}>
          <Logo size={64} />
          <h1 style={{ margin: 0, font: "400 26px/1 var(--font-body)", color: "var(--brand-strong)" }}>SwapOwl</h1>
          <p style={{ margin: 0, color: "var(--text-muted)", textAlign: "center" }}>Концепт для линейки Swap «Флагмана»</p>
        </div>
        <Card title="Вход">
          <form onSubmit={submit} style={{ display: "grid", gap: 12 }}>
            <TextInput label="Логин" value={name} onChange={(e) => setName(e.currentTarget.value)} autoComplete="username" required autoFocus />
            <PasswordInput label="Пароль" value={password} onChange={(e) => setPassword(e.currentTarget.value)} autoComplete="current-password" required />
            {error && <p role="alert" style={{ margin: 0, color: "var(--urgency-critical-fg)" }}>{error}</p>}
            <PrimaryButton type="submit" loading={sending} fullWidth>
              {sending ? "Входим…" : "Войти"}
            </PrimaryButton>
          </form>
        </Card>
      </div>
    </main>
  );
}
