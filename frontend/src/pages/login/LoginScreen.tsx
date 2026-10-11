import { useState } from "react";
import type { FormEvent } from "react";
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
    <main style={{ maxWidth: 360, margin: "64px auto", padding: 16, fontFamily: "system-ui, sans-serif" }}>
      <h1>StockSense</h1>
      <p>Войдите, чтобы продолжить.</p>
      <form onSubmit={submit}>
        <p>
          <label>
            Логин
            <br />
            <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="username" required autoFocus />
          </label>
        </p>
        <p>
          <label>
            Пароль
            <br />
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </label>
        </p>
        {error && <p role="alert">{error}</p>}
        <button type="submit" disabled={sending}>
          {sending ? "Входим…" : "Войти"}
        </button>
      </form>
    </main>
  );
}
