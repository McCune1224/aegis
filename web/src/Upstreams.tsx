import { createSignal, For, Show } from "solid-js";
import type { Route, RouteInput, Upstream, UpstreamInput } from "./api";
import CrudActions from "./CrudActions";
import SaveStatus from "./SaveStatus";
import DataTable, { type Column } from "./DataTable";
import { createCrud, duplicateName, editorTitle } from "./crud";
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
   const upstreamCrud = createCrud<string>();
   const routeCrud = createCrud<number>();
   const [name, setName] = createSignal("");
   const [url, setUrl] = createSignal("");
   const [enabled, setEnabled] = createSignal(true);
   const [backup, setBackup] = createSignal(false);
   const [domain, setDomain] = createSignal("");
   const [client, setClient] = createSignal("");
   const [upstreamName, setUpstreamName] = createSignal("");
   const [rowError, setRowError] = createSignal<string>();

   function clearUpstream() {
      setName("");
      setUrl("");
      setEnabled(true);
      setBackup(false);
   }

   function fillUpstream(row: Upstream) {
      setName(row.name);
      setUrl(row.url);
      setEnabled(row.enabled);
      setBackup(row.backup);
   }

   function openUpstreamDrawer() {
      clearUpstream();
      upstreamCrud.openNew();
   }

   function editUpstream(row: Upstream) {
      fillUpstream(row);
      upstreamCrud.openEdit(row.name);
   }

   // The name is the key the upstreams API writes through, so the copy takes a
   // free one and keeps the URL and both flags.
   function duplicateUpstream(row: Upstream) {
      fillUpstream(row);
      setName(duplicateName(row.name, props.upstreams.map((entry) => entry.name)));
      upstreamCrud.openDuplicate(row.name);
   }

   async function submitUpstream(event: SubmitEvent) {
      event.preventDefault();
      if (!name().trim() || !url().trim()) {
         upstreamCrud.setError("an upstream needs a name and a URL");
         return;
      }
      const input: UpstreamInput = { url: url().trim(), enabled: enabled(), backup: backup() };
      const key = name().trim();
      await upstreamCrud.run(async () => {
         await props.onSaveUpstream(key, input);
         upstreamCrud.close();
      });
   }

   function clearRoute() {
      setDomain("");
      setClient("");
      setUpstreamName("");
   }

   function fillRoute(route: Route) {
      setDomain(route.domain);
      setClient(route.client);
      setUpstreamName(route.upstream);
   }

   function openRouteDrawer() {
      clearRoute();
      routeCrud.openNew();
   }

   function editRoute(route: Route) {
      fillRoute(route);
      routeCrud.openEdit(route.id);
   }

   // A route is keyed by an id the database assigns, so a copy is a create with
   // the same domain, client, and upstream.
   function duplicateRoute(route: Route) {
      fillRoute(route);
      routeCrud.openDuplicate(route.id);
   }

   async function submitRoute(event: SubmitEvent) {
      event.preventDefault();
      if (!upstreamName()) {
         routeCrud.setError("a route needs an upstream");
         return;
      }
      const input: RouteInput = { domain: domain().trim(), client: client().trim(), upstream: upstreamName() };
      const editor = routeCrud.editor();
      await routeCrud.run(async () => {
         if (editor.mode === "edit") {
            await props.onUpdateRoute(editor.key, input);
         } else {
            await props.onCreateRoute(input);
         }
         routeCrud.close();
      });
   }

   async function removeUpstream(target: string) {
      setRowError(undefined);
      try {
         await props.onDeleteUpstream(target);
         const editor = upstreamCrud.editor();
         if (editor.mode === "edit" && editor.key === target) {
            upstreamCrud.close();
         }
      } catch (cause) {
         setRowError(String(cause));
      }
   }

   async function removeRoute(id: number) {
      setRowError(undefined);
      try {
         await props.onDeleteRoute(id);
         const editor = routeCrud.editor();
         if (editor.mode === "edit" && editor.key === id) {
            routeCrud.close();
         }
      } catch (cause) {
         setRowError(String(cause));
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
            <CrudActions
               testid="upstream"
               label={row.name}
               busy={upstreamCrud.busy()}
               onEdit={() => editUpstream(row)}
               onDuplicate={() => duplicateUpstream(row)}
               onDelete={() => void removeUpstream(row.name)}
            />
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
            <CrudActions
               testid="route"
               label={route.domain || "any domain"}
               busy={routeCrud.busy()}
               onEdit={() => editRoute(route)}
               onDuplicate={() => duplicateRoute(route)}
               onDelete={() => void removeRoute(route.id)}
            />
         ),
      },
   ];

   return (
      <>
         <div class="view-scroll" style={{ padding: "18px clamp(14px, 2.5vw, 30px) 44px" }}>
            <div class="view-inner">
               <Show when={rowError()}>
                  <p class="alert-line error-line" role="alert" style={{ margin: "0 0 14px" }}>
                     {rowError()}
                  </p>
               </Show>
               <section class="sheet">
                  <div class="sheet-head">
                     <h2>Upstreams</h2>
                     <button type="button" class="btn-mini" data-testid="upstream-new" onClick={openUpstreamDrawer}>
                        + New upstream
                     </button>
                  </div>
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
                     <button type="button" class="btn-mini" data-testid="route-new" onClick={openRouteDrawer}>
                        + New route
                     </button>
                  </div>
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
            open={upstreamCrud.editor().mode !== "closed"}
            title={editorTitle(upstreamCrud.editor(), "upstream")}
            onClose={upstreamCrud.close}
         >
            <form onSubmit={(event) => void submitUpstream(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="upstream-name"
                     value={name()}
                     disabled={upstreamCrud.editor().mode === "edit"}
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
               {upstreamCrud.error() ? <p class="error">{upstreamCrud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <SaveStatus state={upstreamCrud.saveState()} />
                  <button type="submit" class="btn" data-testid="upstream-save" disabled={upstreamCrud.busy()}>
                     Save
                  </button>
               </div>
            </form>
         </Drawer>

         <Drawer
            open={routeCrud.editor().mode !== "closed"}
            title={editorTitle(routeCrud.editor(), "route")}
            onClose={routeCrud.close}
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
               {routeCrud.error() ? <p class="error">{routeCrud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <SaveStatus state={routeCrud.saveState()} />
                  <button type="submit" class="btn" data-testid="route-save" disabled={routeCrud.busy()}>
                     Save
                  </button>
               </div>
            </form>
         </Drawer>
      </>
   );
}
