import { createSignal, For, Show } from "solid-js";
import type { Rule, RuleInput } from "./api";

const kinds = ["exact", "subdomains", "wildcard", "regex", "cidr"];
const actions = ["block", "allow"];

const placeholders: Record<string, string> = {
  exact: "ads.example.com",
  subdomains: "ads.example.com",
  wildcard: "*.ads.example",
  regex: "^ads[0-9]+\\.example$",
  cidr: "10.0.0.0/16",
};

function placeholderFor(kind: string): string {
  return placeholders[kind] ?? "ads.example.com";
}

type Props = {
  rules: Rule[];
  schedules: string[];
  clients: string[];
  onCreate: (input: RuleInput) => Promise<void>;
  onUpdate: (id: number, input: RuleInput) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
};

// createdTime is the calendar day the rule was written, in the reader's zone.
function createdTime(rule: Rule): string {
  if (!rule.created) {
    return "";
  }
  const parsed = new Date(rule.created);
  return Number.isNaN(parsed.getTime()) ? rule.created : parsed.toLocaleDateString();
}

// summary is the row's one line of context. Joining a list rather than
// concatenating the parts is what stopped it printing "subdomains9/20/2026".
function summary(rule: Rule): string {
  return [
    rule.kind,
    rule.schedule ? `schedule ${rule.schedule}` : "",
    rule.client ? `client ${rule.client}` : "",
    createdTime(rule),
  ]
    .filter(Boolean)
    .join(" · ");
}

export default function Rules(props: Props) {
  const [domain, setDomain] = createSignal("");
  const [kind, setKind] = createSignal("exact");
  const [action, setAction] = createSignal("block");
  const [schedule, setSchedule] = createSignal("");
  const [client, setClient] = createSignal("");
  const [notes, setNotes] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setDomain("");
    setKind("exact");
    setAction("block");
    setSchedule("");
    setClient("");
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
      const input: RuleInput = { domain: domain(), kind: kind(), action: action(), notes: notes() };
      if (schedule()) {
        input.schedule = schedule();
      }
      if (client()) {
        input.client = client();
      }
      await props.onCreate(input);
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
                <span class="muted">{summary(rule)}</span>
                <Show when={rule.notes}>
                  <span class="note">{rule.notes}</span>
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
                <button type="button" class="btn-danger" data-testid="rule-delete" onClick={() => void remove(rule.id)}>
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
          Domain or pattern
          <input
            data-testid="rule-domain"
            value={domain()}
            placeholder={placeholderFor(kind())}
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
          Schedule
          <select
            data-testid="rule-schedule"
            value={schedule()}
            onInput={(event) => setSchedule(event.currentTarget.value)}
          >
            <option value="">always</option>
            <For each={props.schedules}>{(name) => <option value={name}>{name}</option>}</For>
          </select>
        </label>
        <label>
          Client
          <select
            data-testid="rule-client"
            value={client()}
            onInput={(event) => setClient(event.currentTarget.value)}
          >
            <option value="">all clients</option>
            <For each={props.clients}>{(name) => <option value={name}>{name}</option>}</For>
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
