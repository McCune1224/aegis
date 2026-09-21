import { createSignal, For, Show } from "solid-js";
import type { Rewrite } from "./api";

type Props = {
  rewrites: Rewrite[];
  onSave: (pattern: string, target: string) => Promise<void>;
  onDelete: (pattern: string) => Promise<void>;
};

export default function Rewrites(props: Props) {
  const [pattern, setPattern] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!pattern().trim()) {
      setError("a rewrite needs a pattern");
      return;
    }
    if (!target().trim()) {
      setError("a rewrite needs a target");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(pattern().trim(), target().trim());
      setPattern("");
      setTarget("");
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove(pattern: string) {
    setError(undefined);
    try {
      await props.onDelete(pattern);
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <section class="panel">
      <ul>
        <For each={props.rewrites}>
          {(rewrite) => (
            <li data-testid="rewrite-row">
              <div>
                <strong>{rewrite.pattern}</strong>
                <span class="muted">to {rewrite.target}</span>
              </div>
              <div class="row-actions">
                <button
                  type="button"
                  class="btn-danger"
                  data-testid="rewrite-delete"
                  onClick={() => void remove(rewrite.pattern)}
                >
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>New rewrite</h2>
        <label>
          Name
          <input
            data-testid="rewrite-pattern"
            value={pattern()}
            placeholder="nas.local or *.nas.local"
            onInput={(event) => setPattern(event.currentTarget.value)}
          />
        </label>
        <label>
          Target
          <input
            data-testid="rewrite-target"
            value={target()}
            placeholder="192.168.1.50 or another name"
            onInput={(event) => setTarget(event.currentTarget.value)}
          />
        </label>
        <div class="row-actions">
          <button type="submit" data-testid="rewrite-save" disabled={busy()}>
            Save
          </button>
        </div>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
      <p class="muted">
        A name rewrite answers from here no matter what the rules say. A rewrite to another
        name returns that name, and the rules still apply to it.
      </p>
    </section>
  );
}
