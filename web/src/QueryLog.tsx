import { createSignal, For, Show } from "solid-js";
import { createRule, listQueries, type QueryEntry, type Rule } from "./api";

type Props = {
  entries: QueryEntry[];
  live: boolean;
  onToggleLive: (live: boolean) => void;
  onReload: () => Promise<void>;
  onRuleAdded: (rule: Rule) => Promise<void>;
};

const limits = [100, 500, 2000];

export default function QueryLog(props: Props) {
  const [client, setClient] = createSignal("");
  const [name, setName] = createSignal("");
  const [verdict, setVerdict] = createSignal("");
  const [limit, setLimit] = createSignal(500);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string>();

  async function apply() {
    setBusy(true);
    setError(undefined);
    try {
      await listQueries({
        client: client() || undefined,
        name: name() || undefined,
        verdict: verdict() || undefined,
        limit: limit(),
      });
      await props.onReload();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function decide(entry: QueryEntry, action: "allow" | "block") {
    setError(undefined);
    try {
      const rule = await createRule({ domain: entry.name, kind: "subdomains", action });
      await props.onRuleAdded(rule);
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <div class="screen-inner wide">
      <section class="panel">
        <header>
          <h2>Filters</h2>
          <label class="toggle">
            <input
              type="checkbox"
              data-testid="log-live"
              checked={props.live}
              onInput={(event) => props.onToggleLive(event.currentTarget.checked)}
            />
            <span>live</span>
          </label>
        </header>
        <form class="filters" onSubmit={(event) => event.preventDefault()}>
          <label>
            Client
            <input
              data-testid="filter-client"
              placeholder="192.168.1.50"
              value={client()}
              onInput={(event) => setClient(event.currentTarget.value)}
            />
          </label>
          <label>
            Name
            <input
              data-testid="filter-name"
              placeholder="example.com"
              value={name()}
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </label>
          <label>
            Verdict
            <select data-testid="filter-verdict" value={verdict()} onInput={(event) => setVerdict(event.currentTarget.value)}>
              <option value="">all</option>
              <option value="allow">allowed</option>
              <option value="block">blocked</option>
            </select>
          </label>
          <label>
            Limit
            <select value={limit()} onInput={(event) => setLimit(Number(event.currentTarget.value))}>
              <For each={limits}>{(value) => <option value={value}>{value}</option>}</For>
            </select>
          </label>
          <button type="submit" class="btn-solid" data-testid="filter-apply" disabled={busy()} onClick={() => void apply()}>
            Apply
          </button>
        </form>
      </section>

      <Show when={error()}>
        <p class="error">{error()}</p>
      </Show>

      <section class="panel">
        <table>
          <thead>
            <tr>
              <th>Time</th>
              <th>Client</th>
              <th>Name</th>
              <th>Type</th>
              <th>Verdict</th>
              <th>Rule</th>
              <th />
            </tr>
          </thead>
          <tbody data-testid="log-rows">
            <For each={props.entries.slice(0, limit())}>
              {(entry) => (
                <tr data-testid="log-row">
                  <td class="muted">{new Date(entry.time).toLocaleTimeString()}</td>
                  <td class="name">{entry.client}</td>
                  <td class="name">{entry.name}</td>
                  <td class="muted">{entry.type}</td>
                  <td>
                    <span class={`badge ${entry.verdict === "block" ? "block" : "allow"}`}>{entry.verdict}</span>
                  </td>
                  <td class="selectors">{entry.rule ?? ""}</td>
                  <td>
                    <Show
                      when={entry.verdict === "block"}
                      fallback={
                        <button type="button" class="btn-ghost" onClick={() => void decide(entry, "block")}>
                          Block
                        </button>
                      }
                    >
                      <button type="button" class="btn-ghost" onClick={() => void decide(entry, "allow")}>
                        Allow
                      </button>
                    </Show>
                  </td>
                </tr>
              )}
            </For>
            <Show when={props.entries.length === 0}>
              <tr>
                <td colspan={7} class="empty">
                  no queries recorded yet
                </td>
              </tr>
            </Show>
          </tbody>
        </table>
      </section>
    </div>
  );
}
