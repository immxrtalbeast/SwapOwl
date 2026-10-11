import { useState } from "react";
import type { FormEvent } from "react";
import { Alert, PasswordInput, Stack } from "@mantine/core";
import { IconCircleCheck } from "@tabler/icons-react";
import { api } from "../../api/client";
import { useAuth } from "../../auth/AuthContext";
import { roleLabel } from "../../auth/roles";
import { Card, EmptyState, LoadingState, PrimaryButton, ScreenTitle } from "../../components";
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

  if (state.kind !== "user") return <LoadingState rows={3} label="Загрузка аккаунта" />;
  if (state.login === null) {
    return (
      <section aria-labelledby="account-title">
        <ScreenTitle>Аккаунт</ScreenTitle>
        <Card>
          <EmptyState title="Личной учётной записи нет" text="Стенд открыт без входа, менять нечего." />
        </Card>
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
      <ScreenTitle>Аккаунт</ScreenTitle>
      <p style={{ margin: "0 0 16px", font: "var(--type-body)" }}>
        Вы вошли как <strong>{state.login}</strong> ({roleLabel[state.role]}).
      </p>
      <Card title="Смена пароля" style={{ maxWidth: 480 }}>
        <form onSubmit={(e) => void submit(e)} aria-label="Смена пароля">
          <Stack gap="md">
            <PasswordInput label="Текущий пароль" value={oldPw} onChange={(e) => setOldPw(e.currentTarget.value)} autoComplete="current-password" required />
            <PasswordInput label="Новый пароль" description="От 8 до 72 байт" value={newPw} onChange={(e) => setNewPw(e.currentTarget.value)} autoComplete="new-password" required />
            <PasswordInput label="Повторите новый пароль" value={repeat} onChange={(e) => setRepeat(e.currentTarget.value)} autoComplete="new-password" required />
            <p style={{ margin: 0, font: "var(--type-small)", color: "var(--text-muted)" }}>
              После смены остальные ваши сессии (на других устройствах) закроются.
            </p>
            <div>
              <PrimaryButton type="submit" loading={busy}>Сменить пароль</PrimaryButton>
            </div>
            {error && <Alert color="red" role="alert">{error}</Alert>}
            {done && (
              <Alert color="green" role="status" data-testid="password-changed" icon={<IconCircleCheck size={18} aria-hidden="true" />}>
                Пароль изменён.
              </Alert>
            )}
          </Stack>
        </form>
      </Card>
    </section>
  );
}
