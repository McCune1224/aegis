import { createSignal, For, Show } from "solid-js";
import type { Route, RouteInput, Upstream, UpstreamInput } from "./api";

type Props = {
  upstreams: Upstream[];
  routes: Route[];
  onSaveUpstream: (name: string, input: UpstreamInput) => Promise<void>;
  onDeleteUpstream: (name: string) => Promise<void>;
  onCreateRoute: (input: RouteInput) => Promise<void>;
  onUpdateRoute: (id: number, input: RouteInput) => Promise<void>;
  onDeleteRoute: (id: number) => Promise<void>;
};

export function formatLatency(ms: number): string {
  if (ms <= 0) {
    return "unmeasured";
  }
  if (ms < 10) {
    return `${ms.toFixed(1)} ms`;
  }
  return `${Math.round(ms)} ms`;
}

export function health(upstream: Upstream): { label: string; kind: "allow" | "block" | "kind" } {
  if (upstream.down) {
    return { label: "down", kind: "block" };
  }
  if (upstream.failures > 0) {
    return { label: `failing ×${upstream.failures}`, kind: "block" };
  }
  if (!upstream.enabled) {
    return { label: "disabled", kind: "kind" };
  }
  return { label: "ok", kind: "allow" };
}

export default function Upstreams(props: Props) {
  const [name, setName] = createSignal("");
  const [url, setUrl] = createSignal("");
  const [enabled, setEnabled] = createSignal(true);
  const [backup, setBackup] = createSignal(false);
  const [editing, setEditing] = createSignal<string>();
  const [upstreamError, setUpstreamError] = createSignal<string>();
  const [upstreamBusy, setUpstreamBusy] = createSignal(false);
  const [domain, setDomain] = createSignal("");
  const [client, setClient] = createSignal("");
  const [upstreamName, setUpstreamName] = createSignal("");
  const [routeEditing, setRouteEditing] = createSignal<number>();
  const [routeError, setRouteError] = createSignal<string>();
  const [routeBusy, setRouteBusy] = createSignal(false);

  function resetUpstream() {
    setName("");
    setUrl("");
    setEnabled(true);
    setBackup(false);
    setEditing(undefined);
  }

  function editUpstream(row: Upstream) {
    setName(row.name);
    setUrl(row.url);
    setEnabled(row.enabled);
    setBackup(row.backup);
    setEditing(row.name);
    setUpstreamError(undefined);
  }

  async function submitUpstream(event: SubmitEvent) {
    event.preventDefault();
    if (!name().trim() || !url().trim()) {
      setUpstreamError("an upstream needs a name and a URL");
      return;
    }
    setUpstreamBusy(true);
    setUpstreamError(undefined);
    try {
      await props.onSaveUpstream(name().trim(), { url: url().trim(), enabled: enabled(), backup: backup() });
      resetUpstream();
    } catch (cause) {
      setUpstreamError(String(cause));
    } finally {
      setUpstreamBusy(false);
    }
  }

  async function removeUpstream(target: string) {
    setUpstreamError(undefined);
    try {
      await props.onDeleteUpstream(target);
    } catch (cause) {
      setUpstreamError(String(cause));
    }
  }

  function resetRoute() {
    setDomain("");
    setClient("");
    setUpstreamName("");
    setRouteEditing(undefined);
  }

  function editRoute(route: Route) {
    setDomain(route.domain);
    setClient(route.client);
    setUpstreamName(route.upstream);
    setRouteEditing(route.id);
    setRouteError(undefined);
  }

  async function submitRoute(event: SubmitEvent) {
    event.preventDefault();
    if (!upstreamName()) {
      setRouteError("a route needs an upstream");
      return;
    }
    setRouteBusy(true);
    setRouteError(undefined);
    try {
      const input: RouteInput = { domain: domain().trim(), client: client().trim(), upstream: upstreamName() };
      const id = routeEditing();
      if (id === undefined) {
        await props.onCreateRoute(input);
      } else {
        await props.onUpdateRoute(id, input);
      }
      resetRoute();
    } catch (cause) {
      setRouteError(String(cause));
    } finally {
      setRouteBusy(false);
    }
  }

  async function removeRoute(id: number) {
    setRouteError(undefined);
    try {
      await props.onDeleteRoute(id);
    } catch (cause) {
      setRouteError(String(cause));
    }
  }

  return (
    <>
      <section class="panel">
        <header>
          <h2>Upstreams</h2>
        </header>
        <ul>
          <For each={props.upstreams}>
            {(row) => (
              <li data-testid="upstream-row">
                <div>
                  <strong>{row.name}</strong>
                  <span class="selectors">{row.url}</span>
                  <span class="muted">
                    {formatLatency(row.latency_ms)}
                    {row.failures > 0 ? ` · ${row.failures} failures` : ""}
                    {row.backup ? " · backup" : ""}
                  </span>
                </div>
                <div class="row-actions">
                  <span class={`badge ${health(row).kind}`} data-testid="upstream-health">
                    {health(row).label}
                  </span>
                  <button
                    type="button"
                    class="btn-ghost"
                    data-testid="upstream-edit"
                    onClick={() => editUpstream(row)}
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    class="btn-danger"
                    data-testid="upstream-delete"
                    onClick={() => void removeUpstream(row.name)}
                  >
                    Delete
                  </button>
                </div>
              </li>
            )}
          </For>
          <Show when={props.upstreams.length === 0}>
            <li class="empty">no upstreams stored</li>
          </Show>
        </ul>

        <form onSubmit={(event) => void submitUpstream(event)}>
          <h2>{editing() ? `Edit ${editing()}` : "New upstream"}</h2>
          <label>
            Name
            <input
              data-testid="upstream-name"
              value={name()}
              disabled={editing() !== undefined}
              placeholder="quadrant"
              onInput={(event) => setName(event.currentTarget.value)}
            />
          </label>
          <label>
            URL
            <input
              data-testid="upstream-url"
              value={url()}
              placeholder="8.8.8.8:53 or https://dns.example.com/dns-query"
              onInput={(event) => setUrl(event.currentTarget.value)}
            />
          </label>
          <label class="toggle">
            <input
              type="checkbox"
              data-testid="upstream-enabled"
              checked={enabled()}
              onInput={(event) => setEnabled(event.currentTarget.checked)}
            />
            <span>enabled</span>
          </label>
          <label class="toggle">
            <input
              type="checkbox"
              data-testid="upstream-backup"
              checked={backup()}
              onInput={(event) => setBackup(event.currentTarget.checked)}
            />
            <span>backup</span>
          </label>
          <div class="row-actions">
            <button type="submit" data-testid="upstream-save" disabled={upstreamBusy()}>
              Save
            </button>
          </div>
          {upstreamError() ? <p class="error">{upstreamError()}</p> : null}
        </form>
      </section>

      <section class="panel">
        <header>
          <h2>Routes</h2>
        </header>
        <ul>
          <For each={props.routes}>
            {(route) => (
              <li data-testid="route-row">
                <div>
                  <strong>{route.domain || "any domain"}</strong>
                  <span class="muted">
                    client {route.client || "any"} to {route.upstream}
                  </span>
                </div>
                <div class="row-actions">
                  <button
                    type="button"
                    class="btn-ghost"
                    data-testid="route-edit"
                    onClick={() => editRoute(route)}
                  >
                    Edit
                  </button>
                  <button
                    type="button"
                    class="btn-danger"
                    data-testid="route-delete"
                    onClick={() => void removeRoute(route.id)}
                  >
                    Delete
                  </button>
                </div>
              </li>
            )}
          </For>
          <Show when={props.routes.length === 0}>
            <li class="empty">no routes yet</li>
          </Show>
        </ul>

        <form onSubmit={(event) => void submitRoute(event)}>
          <h2>{routeEditing() !== undefined ? "Edit route" : "New route"}</h2>
          <label>
            Domain
            <input
              data-testid="route-domain"
              value={domain()}
              placeholder="ads.example.com or blank for any"
              onInput={(event) => setDomain(event.currentTarget.value)}
            />
          </label>
          <label>
            Client
            <input
              data-testid="route-client"
              value={client()}
              placeholder="client name or blank for any"
              onInput={(event) => setClient(event.currentTarget.value)}
            />
          </label>
          <label>
            Upstream
            <select
              data-testid="route-upstream"
              value={upstreamName()}
              onInput={(event) => setUpstreamName(event.currentTarget.value)}
            >
              <option value="">choose an upstream</option>
              <For each={props.upstreams}>{(row) => <option value={row.name}>{row.name}</option>}</For>
            </select>
          </label>
          <div class="row-actions">
            <button type="submit" data-testid="route-save" disabled={routeBusy()}>
              Save
            </button>
          </div>
          {routeError() ? <p class="error">{routeError()}</p> : null}
        </form>
      </section>
    </>
  );
}
