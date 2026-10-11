import { useCallback, useState } from "react";
import type { FormEvent } from "react";
import { api } from "../../api/client";
import type { ApiKeyInfo, CreatedApiKey, UserAccount } from "../../api/types";
import { RoleGate } from "../../auth/AuthContext";
import { roleLabel } from "../../auth/roles";
import type { Role } from "../../auth/roles";
import { useLive } from "../../app/useLive";
import { createUserError, deleteUserError } from "./errors";

type Data = { users: UserAccount[]; keys: ApiKeyInfo[] };
type State = { kind: "loading" } | { kind: "error"; message: string } | ({ kind: "ready" } & Data);

const msg = (e: unknown, fallback: string) => (e instanceof Error ? e.message : fallback);

export function UsersPage() {
  return (
    <RoleGate min="admin">
      <UsersAdmin />
    </RoleGate>
  );
}

function UsersAdmin() {
  const [state, setState] = useState<State>({ kind: "loading" });
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [users, keys] = await Promise.all([api.users(), api.apiKeys()]);
      setState({ kind: "ready", users, keys });
    } catch (e) {
      setState({ kind: "error", message: msg(e, "Не удалось загрузить данные") });
    }
  }, []);

  useLive(load, ["stand.reset"]);

  return (
    <section aria-labelledby="users-title">
      <h2 id="users-title">Пользователи и ключи</h2>
      <p><a href="#/account">Аккаунт: сменить свой пароль</a></p>
      {notice && <p role="alert" data-testid="users-notice">{notice}</p>}
      {state.kind === "loading" && <p role="status">Загрузка…</p>}
      {state.kind === "error" && (
        <div role="alert">
          <p>Ошибка: {state.message}</p>
          <button onClick={() => { setState({ kind: "loading" }); void load(); }}>Повторить</button>
        </div>
      )}
      {state.kind === "ready" && (
        <>
          <UsersBlock users={state.users} reload={load} setNotice={setNotice} />
          <KeysBlock keys={state.keys} reload={load} setNotice={setNotice} />
        </>
      )}
    </section>
  );
}

interface BlockProps {
  reload: () => Promise<void>;
  setNotice: (m: string | null) => void;
}

function UsersBlock({ users, reload, setNotice }: BlockProps & { users: UserAccount[] }) {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const [formError, setFormError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setFormError(null);
    setNotice(null);
    try {
      await api.createUser({ login: login.trim(), password, role });
      setLogin("");
      setPassword("");
      await reload();
    } catch (e) {
      setFormError(createUserError(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(u: UserAccount) {
    if (!window.confirm(`Удалить пользователя «${u.login}»?`)) return;
    setNotice(null);
    try {
      await api.deleteUser(u.id);
      await reload();
    } catch (e) {
      setNotice(deleteUserError(e));
    }
  }

  return (
    <div>
      <h3>Пользователи</h3>
      {users.length === 0 ? (
        <p>Пользователей нет.</p>
      ) : (
        <table>
          <thead>
            <tr><th scope="col">Логин</th><th scope="col">Роль</th><th scope="col">Создан</th><th scope="col"><span className="sr-only">Действия</span></th></tr>
          </thead>
          <tbody>
            {users.map((u) => (
              <tr key={u.id} data-testid={`user-${u.id}`}>
                <td>{u.login}</td>
                <td>{roleLabel[u.role]}</td>
                <td>{new Date(u.created_at).toLocaleString("ru-RU")}</td>
                <td><button onClick={() => void remove(u)} aria-label={`Удалить пользователя ${u.login}`}>Удалить</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <form onSubmit={(e) => void submit(e)} aria-label="Новый пользователь" style={{ marginTop: 12 }}>
        <label>Логин <input value={login} onChange={(e) => setLogin(e.target.value)} required maxLength={128} /></label>{" "}
        <label>Пароль <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={8} maxLength={72} autoComplete="new-password" /></label>{" "}
        <label>
          Роль{" "}
          <select value={role} onChange={(e) => setRole(e.target.value as Role)}>
            {(["viewer", "operator", "admin"] as Role[]).map((r) => <option key={r} value={r}>{roleLabel[r]}</option>)}
          </select>
        </label>{" "}
        <button type="submit" disabled={busy}>Создать пользователя</button>
        {formError && <p role="alert">{formError}</p>}
      </form>
    </div>
  );
}

function KeysBlock({ keys, reload, setNotice }: BlockProps & { keys: ApiKeyInfo[] }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<ApiKeyInfo["role"]>("operator");
  const [formError, setFormError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [copied, setCopied] = useState<"ok" | "fail" | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setFormError(null);
    setNotice(null);
    try {
      const k = await api.createApiKey({ name: name.trim(), role });
      setCreated(k);
      setCopied(null);
      setName("");
      await reload();
    } catch (e) {
      setFormError(msg(e, "Не удалось создать ключ"));
    } finally {
      setBusy(false);
    }
  }

  async function copy() {
    if (!created) return;
    try {
      await navigator.clipboard.writeText(created.key);
      setCopied("ok");
    } catch {
      setCopied("fail");
    }
  }

  async function revoke(k: ApiKeyInfo) {
    if (!window.confirm(`Отозвать ключ «${k.name}»? Адаптер с этим ключом перестанет работать.`)) return;
    setNotice(null);
    try {
      await api.deleteApiKey(k.id);
      if (created?.id === k.id) setCreated(null);
      await reload();
    } catch (e) {
      setNotice(msg(e, "Не удалось отозвать ключ"));
    }
  }

  return (
    <div style={{ marginTop: 24 }}>
      <h3>Ключи интеграции</h3>
      <p>Ключ для адаптера: заголовок <code>Authorization: Bearer &lt;ключ&gt;</code>. Роль — просмотр или оператор.</p>
      {created && (
        <div role="status" data-testid="created-key" style={{ border: "1px solid currentColor", padding: 12, marginBottom: 12 }}>
          <strong>Ключ «{created.name}» создан. Скопируйте его сейчас — позже он не будет показан.</strong>
          <div>
            <code data-testid="key-value" style={{ wordBreak: "break-all" }}>{created.key}</code>
          </div>
          <button onClick={() => void copy()}>Скопировать</button>{" "}
          <button onClick={() => setCreated(null)}>Я сохранил ключ</button>
          {copied === "ok" && <span> Скопировано.</span>}
          {copied === "fail" && <span> Не удалось скопировать — выделите ключ вручную.</span>}
        </div>
      )}
      {keys.length === 0 ? (
        <p>Ключей нет.</p>
      ) : (
        <table>
          <thead>
            <tr><th scope="col">Название</th><th scope="col">Роль</th><th scope="col">Создан</th><th scope="col"><span className="sr-only">Действия</span></th></tr>
          </thead>
          <tbody>
            {keys.map((k) => (
              <tr key={k.id} data-testid={`key-${k.id}`}>
                <td>{k.name}</td>
                <td>{roleLabel[k.role]}</td>
                <td>{new Date(k.created_at).toLocaleString("ru-RU")}</td>
                <td><button onClick={() => void revoke(k)} aria-label={`Отозвать ключ ${k.name}`}>Отозвать</button></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <form onSubmit={(e) => void submit(e)} aria-label="Новый ключ" style={{ marginTop: 12 }}>
        <label>Название <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={128} placeholder="iiko-адаптер" /></label>{" "}
        <label>
          Роль{" "}
          <select value={role} onChange={(e) => setRole(e.target.value as ApiKeyInfo["role"])}>
            <option value="viewer">{roleLabel.viewer}</option>
            <option value="operator">{roleLabel.operator}</option>
          </select>
        </label>{" "}
        <button type="submit" disabled={busy}>Создать ключ</button>
        {formError && <p role="alert">{formError}</p>}
      </form>
    </div>
  );
}
