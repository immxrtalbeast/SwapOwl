import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { roleLabel } from "../../auth/roles";
import { changePasswordError, validatePasswordChange } from "./validate";

/** Экран «Аккаунт»: смена собственного пароля (POST /api/auth/password). */
export function AccountPage() {
  const { state } = useAuth();
  const [oldPw, setOldPw] = useState("");
  const [newPw, setNewPw] = useState("");
  const [repeat, setRepeat] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const [busy, setBusy] = useState(false);

  if (state.kind !== "user") return <p role="status">Загрузка…</p>;
  if (state.login === null) {
    return (
      <section aria-labelledby="account-title">
        <h2 id="account-title">Аккаунт</h2>
        <p>Стенд открыт без входа, личной учётной записи нет — менять нечего.</p>
      </section>
    );
  }

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setDone(false);
    const invalid = validatePasswordChange(oldPw, newPw, repeat);
    if (invalid) {
      setError(invalid);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await api.changePassword(oldPw, newPw);
      setOldPw("");
      setNewPw("");
      setRepeat("");
      setDone(true);
    } catch (e) {
      setError(changePasswordError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby="account-title">
      <h2 id="account-title">Аккаунт</h2>
      <p>
        Вы вошли как <strong>{state.login}</strong> ({roleLabel[state.role]}).
      </p>
      <h3>Смена пароля</h3>
      <form onSubmit={(e) => void submit(e)} aria-label="Смена пароля">
        <p>
          <label>
            Текущий пароль <br />
            <input type="password" value={oldPw} onChange={(e) => setOldPw(e.target.value)} autoComplete="current-password" required />
          </label>
        </p>
        <p>
          <label>
            Новый пароль <br />
            <input type="password" value={newPw} onChange={(e) => setNewPw(e.target.value)} autoComplete="new-password" required />
          </label>
        </p>
        <p>
          <label>
            Повторите новый пароль <br />
            <input type="password" value={repeat} onChange={(e) => setRepeat(e.target.value)} autoComplete="new-password" required />
          </label>
        </p>
        <p>После смены остальные ваши сессии (на других устройствах) закроются.</p>
        <button type="submit" disabled={busy}>{busy ? "Сохраняем…" : "Сменить пароль"}</button>
        {error && <p role="alert">{error}</p>}
        {done && <p role="status" data-testid="password-changed">Пароль изменён.</p>}
      </form>
    </section>
  );
}
