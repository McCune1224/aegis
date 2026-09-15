import { createSignal } from "solid-js";

type Props = {
  onSignIn: (password: string) => Promise<void>;
};

export default function Login(props: Props) {
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      await props.onSignIn(password());
      setPassword("");
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={(event) => void submit(event)}>
      <h2>Sign in</h2>
      <label>
        Admin password
        <input
          type="password"
          data-testid="admin-password"
          value={password()}
          onInput={(event) => setPassword(event.currentTarget.value)}
        />
      </label>
      <div class="row-actions">
        <button type="submit" data-testid="admin-signin" disabled={busy()}>
          Sign in
        </button>
      </div>
      {error() ? <p class="error">{error()}</p> : null}
    </form>
  );
}
