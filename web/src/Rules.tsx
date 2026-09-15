import { createSignal, For, Show } from "solid-js";
import type { Rule, RuleInput } from "./api";

const kinds = ["exact", "subdomains"];
const actions = ["block", "allow"];

type Props = {
  rules: Rule[];
  onCreate: (input: RuleInput) => Promise<void>;
  onUpdate: (id: number, input: RuleInput) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

function createdTime(rule: Rule): string {
  if (!rule.created) {
    return "";
  }
  const parsed = new Date(rule.created);
  return Number.isNaN(parsed.getTime()) ? rule.created : parsed.toLocaleDateString();
}

export default function Rules(props: Props) {
  const [domain, setDomain] = createSignal("");
  const [kind, setKind] = createSignal("exact");
  const [action, setAction] = createSignal("block");
  const [notes, setNotes] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setDomain("");
    setKind("exact");
    setAction("block");
    setNotes("");
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!domain()) {
      setError("a rule needs a domain");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onCreate({ domain: domain(), kind: kind(), action: action(), notes: notes() });
      reset();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function changeAction(rule: Rule, next: string) {
    setError(undefined);
    try {
      await props.onUpdate(rule.id, { action: next });
    } catch (cause) {
      setError(String(cause));
    }
  }

  async function remove(id: number) {
    setError(undefined);
    try {
      await props.onDelete(id);
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <section class="panel">
      <ul>
        <For each={props.rules}>
          {(rule) => (
            <li data-testid="rule-row">
              <div>
                <strong>{rule.domain}</strong>
                <span class="muted">
                  {rule.kind} {createdTime(rule)}
                </span>
                <Show when={rule.notes}>
                  <span class="selectors">{rule.notes}</span>
                </Show>
              </div>
              <div class="row-actions">
                <select
                  data-testid="rule-action"
                  value={rule.action}
                  onChange={(event) => void changeAction(rule, event.currentTarget.value)}
                >
                  <For each={actions}>{(value) => <option value={value}>{value}</option>}</For>
                </select>
                <button type="button" data-testid="rule-delete" onClick={() => void remove(rule.id)}>
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>New rule</h2>
        <label>
          Domain
          <input
            data-testid="rule-domain"
            value={domain()}
            placeholder="ads.example.com"
            onInput={(event) => setDomain(event.currentTarget.value)}
          />
        </label>
        <label>
          Match
          <select
            data-testid="rule-kind"
            value={kind()}
            onInput={(event) => setKind(event.currentTarget.value)}
          >
            <For each={kinds}>{(value) => <option value={value}>{value}</option>}</For>
          </select>
        </label>
        <label>
          Action
          <select
            data-testid="rule-new-action"
            value={action()}
            onInput={(event) => setAction(event.currentTarget.value)}
          >
            <For each={actions}>{(value) => <option value={value}>{value}</option>}</For>
          </select>
        </label>
        <label>
          Notes
          <input
            data-testid="rule-notes"
            value={notes()}
            placeholder="why this rule exists"
            onInput={(event) => setNotes(event.currentTarget.value)}
          />
        </label>
        <div class="row-actions">
          <button type="submit" data-testid="rule-save" disabled={busy()}>
            Save
          </button>
        </div>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
    </section>
  );
}
