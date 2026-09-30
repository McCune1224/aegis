import { createSignal, For, Show } from "solid-js";
import type { Route, RouteInput, Upstream, UpstreamInput } from "./api";
import DataTable, { type Column } from "./DataTable";
import Drawer from "./Drawer";

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
  const [upstreamDrawer, setUpstreamDrawer] = createSignal(false);
  const [domain, setDomain] = createSignal("");
  const [client, setClient] = createSignal("");
  const [upstreamName, setUpstreamName] = createSignal("");
  const [routeEditing, setRouteEditing] = createSignal<number>();
  const [routeError, setRouteError] = createSignal<string>();
  const [routeBusy, setRouteBusy] = createSignal(false);
  const [routeDrawer, setRouteDrawer] = createSignal(false);

  function openUpstreamDrawer() {
    setName("");
    setUrl("");
    setEnabled(true);
    setBackup(false);
    setEditing(undefined);
    setUpstreamError(undefined);
    setUpstreamDrawer(true);
  }

  function editUpstream(row: Upstream) {
    setName(row.name);
    setUrl(row.url);
    setEnabled(row.enabled);
    setBackup(row.backup);
    setEditing(row.name);
    setUpstreamError(undefined);
    setUpstreamDrawer(true);
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
      setUpstreamDrawer(false);
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

  function openRouteDrawer() {
    setDomain("");
    setClient("");
    setUpstreamName("");
    setRouteEditing(undefined);
    setRouteError(undefined);
    setRouteDrawer(true);
  }

  function editRoute(route: Route) {
    setDomain(route.domain);
    setClient(route.client);
    setUpstreamName(route.upstream);
    setRouteEditing(route.id);
    setRouteError(undefined);
    setRouteDrawer(true);
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
      setRouteDrawer(false);
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

  const upstreamColumns: Column<Upstream>[] = [
    {
      key: "name",
      label: "Upstream",
      sortable: true,
      value: (row) => row.name,
      render: (row) => (
        <div style={{ display: "flex", "flex-direction": "column", gap: "2px" }}>
          <span class="mono">{row.name}</span>
          <span class="selectors">{row.url}</span>
        </div>
      ),
    },
    {
      key: "latency",
      label: "Latency",
      sortable: true,
      value: (row) => row.latency_ms,
      render: (row) => (
        <span class="muted" style={{ "font-size": "11.5px" }}>
          {formatLatency(row.latency_ms)}
          {row.failures > 0 ? ` · ${row.failures} failures` : ""}
          {row.backup ? " · backup" : ""}
        </span>
      ),
    },
    {
      key: "health",
      label: "Health",
      value: (row) => health(row).label,
      render: (row) => (
        <span class={`badge ${health(row).kind}`} data-testid="upstream-health">
          {health(row).label}
        </span>
      ),
    },
    {
      key: "actions",
      label: "",
      value: () => "",
      render: (row) => (
        <div class="row-actions">
          <button type="button" class="btn-mini" data-testid="upstream-edit" onClick={() => editUpstream(row)}>
            Edit
          </button>
          <button type="button" class="btn-mini" data-testid="upstream-delete" onClick={() => void removeUpstream(row.name)}>
            Delete
          </button>
        </div>
      ),
    },
  ];

  const routeColumns: Column<Route>[] = [
    {
      key: "domain",
      label: "Domain",
      sortable: true,
      value: (route) => route.domain,
      render: (route) => <span class="mono">{route.domain || "any domain"}</span>,
    },
    {
      key: "client",
      label: "Client",
      sortable: true,
      value: (route) => route.client,
      render: (route) => <span class="mono muted">{route.client || "any"}</span>,
    },
    {
      key: "upstream",
      label: "Answers via",
      sortable: true,
      value: (route) => route.upstream,
      render: (route) => <span class="badge info">{route.upstream}</span>,
    },
    {
      key: "actions",
      label: "",
      value: () => "",
      render: (route) => (
        <div class="row-actions">
          <button type="button" class="btn-mini" data-testid="route-edit" onClick={() => editRoute(route)}>
            Edit
          </button>
          <button type="button" class="btn-mini" data-testid="route-delete" onClick={() => void removeRoute(route.id)}>
            Delete
          </button>
        </div>
      ),
    },
  ];

  return (
    <>
      <div class="view-scroll" style={{ padding: "18px clamp(14px, 2.5vw, 30px) 44px" }}>
        <div class="view-inner">
          <section class="sheet">
            <div class="sheet-head">
              <h2>Upstreams</h2>
              <button type="button" class="btn-mini" data-testid="upstream-new" onClick={() => openUpstreamDrawer()}>
                + New upstream
              </button>
            </div>
            <Show when={upstreamError()}>
              <p class="error" style={{ margin: "10px 14px" }}>
                {upstreamError()}
              </p>
            </Show>
            <DataTable
              columns={upstreamColumns}
              rows={props.upstreams}
              rowKey={(row) => row.name}
              testid="upstream-rows"
              rowTestid={() => "upstream-row"}
              empty="no upstreams stored"
            />
          </section>

          <section class="sheet">
            <div class="sheet-head">
              <h2>Routes</h2>
              <button type="button" class="btn-mini" data-testid="route-new" onClick={() => openRouteDrawer()}>
                + New route
              </button>
            </div>
            <Show when={routeError()}>
              <p class="error" style={{ margin: "10px 14px" }}>
                {routeError()}
              </p>
            </Show>
            <DataTable
              columns={routeColumns}
              rows={props.routes}
              rowKey={(route) => route.id}
              testid="route-rows"
              rowTestid={() => "route-row"}
              empty="no routes yet — every query goes to the first enabled upstream"
            />
            <p class="muted" style={{ padding: "0 16px 14px", "font-size": "11.5px" }}>
              A blank domain matches every name and a blank client matches every client, so the router fills the gaps a
              rule leaves open.
            </p>
          </section>
        </div>
      </div>

      <Drawer
        open={upstreamDrawer()}
        title={editing() ? `Edit ${editing()}` : "New upstream"}
        onClose={() => setUpstreamDrawer(false)}
      >
        <form onSubmit={(event) => void submitUpstream(event)} style={{ display: "contents" }}>
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
          {upstreamError() ? <p class="error">{upstreamError()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="upstream-save" disabled={upstreamBusy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>

      <Drawer
        open={routeDrawer()}
        title={routeEditing() !== undefined ? "Edit route" : "New route"}
        onClose={() => setRouteDrawer(false)}
      >
        <form onSubmit={(event) => void submitRoute(event)} style={{ display: "contents" }}>
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
          {routeError() ? <p class="error">{routeError()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="route-save" disabled={routeBusy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
