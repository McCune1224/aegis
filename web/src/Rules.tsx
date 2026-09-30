import { createSignal, For, Show } from "solid-js";
import type { Rule, RuleInput } from "./api";
import DataTable, { type Column } from "./DataTable";
import Drawer from "./Drawer";

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
  const [drawer, setDrawer] = createSignal(false);
  const [rowError, setRowError] = createSignal<string>();

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
      setDomain("");
      setKind("exact");
      setAction("block");
      setSchedule("");
      setClient("");
      setNotes("");
      setDrawer(false);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function changeAction(rule: Rule, next: string) {
    setRowError(undefined);
    try {
      await props.onUpdate(rule.id, { action: next });
    } catch (cause) {
      setRowError(String(cause));
    }
  }

  async function remove(id: number) {
    setRowError(undefined);
    try {
      await props.onDelete(id);
    } catch (cause) {
      setRowError(String(cause));
    }
  }

  const columns: Column<Rule>[] = [
    {
      key: "domain",
      label: "Domain or pattern",
      sortable: true,
      value: (rule) => rule.domain,
      render: (rule) => (
        <div style={{ display: "flex", "flex-direction": "column", gap: "2px" }}>
          <span class="mono">{rule.domain}</span>
          <Show when={rule.notes}>
            <span class="note">{rule.notes}</span>
          </Show>
        </div>
      ),
    },
    {
      key: "kind",
      label: "Match",
      sortable: true,
      value: (rule) => rule.kind,
      render: (rule) => <span class="badge kind">{rule.kind}</span>,
    },
    {
      key: "action",
      label: "Action",
      sortable: true,
      value: (rule) => rule.action,
      render: (rule) => (
        <select
          data-testid="rule-action"
          class="mono"
          style={{ width: "auto", padding: "3px 26px 3px 10px", "border-radius": "999px", "font-size": "11px" }}
          value={rule.action}
          onClick={(event) => event.stopPropagation()}
          onChange={(event) => void changeAction(rule, event.currentTarget.value)}
        >
          <For each={actions}>{(value) => <option value={value}>{value}</option>}</For>
        </select>
      ),
    },
    {
      key: "scope",
      label: "Scope",
      value: (rule) => summary(rule),
      render: (rule) => <span class="muted" style={{ "font-size": "11.5px" }}>{summary(rule)}</span>,
    },
    {
      key: "actions",
      label: "",
      value: () => "",
      render: (rule) => (
        <button
          type="button"
          class="btn-mini"
          data-testid="rule-delete"
          onClick={(event) => {
            event.stopPropagation();
            void remove(rule.id);
          }}
        >
          Delete
        </button>
      ),
    },
  ];

  return (
    <>
      <div class="view">
        <div class="subbar">
          <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
            {props.rules.length} rules
          </h2>
          <Show when={props.schedules.length === 0}>
            <span class="muted" style={{ "font-size": "11.5px" }}>no schedules yet — a rule can still scope to one client</span>
          </Show>
          <button type="button" class="btn" data-testid="rules-new" onClick={() => setDrawer(true)}>
            + New rule
          </button>
        </div>
        <Show when={rowError()}>
          <p class="alert-line error-line" role="alert">
            {rowError()}
          </p>
        </Show>
        <div style={{ flex: "1", "min-height": "0", overflow: "auto" }}>
          <DataTable
            columns={columns}
            rows={props.rules}
            rowKey={(rule) => rule.id}
            testid="rule-rows"
            rowTestid={() => "rule-row"}
            empty="no rules yet — block or allow the first name"
          />
        </div>
      </div>

      <Drawer open={drawer()} title="New rule" onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
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
          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="rule-save" disabled={busy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
