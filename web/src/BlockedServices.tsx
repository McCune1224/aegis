import { createMemo, createSignal, createEffect, For, Show } from "solid-js";
import type { BlockedService, Client, Schedule, ServiceWindow, ServiceWindowInput } from "./api";
import CrudActions from "./CrudActions";
import { createCrud, duplicateName, editorTitle, type SaveState } from "./crud";
import { groupServices, groupState, serviceGroupLabel, toggled, toggledGroup } from "./services";
import Drawer from "./Drawer";
import SaveStatus from "./SaveStatus";

const DAY_LABELS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];

function describeSchedule(schedule: Schedule | undefined): string {
   if (!schedule) {
      return "";
   }
   return schedule.windows
      .map((window) => {
         const days = window.days.length === 7 ? "every day" : window.days.map((day) => DAY_LABELS[day] ?? "?").join(" ");
         return `${days} ${window.start}-${window.end}`;
      })
      .join("; ");
}

// Scope names the layer the sliders on this page edit: a profile blocks a
// service for every client on it, a client blocks one for itself alone. The
// layers add up, so a service blocked by a profile stays blocked for a client
// that has not enabled it here.
export type ServiceScope = { kind: "profile" | "client"; name: string };

type Props = {
   services: BlockedService[];
   serviceGroups: string[];
   profileNames: string[];
   clients: Client[];
   defaultProfile: string;
   schedules: Schedule[];
   windows: ServiceWindow[];
   onSave: (scope: ServiceScope, services: string[]) => Promise<void>;
   onRefreshServices: () => Promise<void>;
   onSaveWindow: (name: string, input: ServiceWindowInput) => Promise<void>;
   onDeleteWindow: (name: string) => Promise<void>;
};

export default function BlockedServices(props: Props) {
   const [scope, setScope] = createSignal<ServiceScope>({
      kind: "profile",
      name: props.defaultProfile || props.profileNames[0] || "",
   });
   const [search, setSearch] = createSignal("");
   const [busy, setBusy] = createSignal(false);
   const [saveState, setSaveState] = createSignal<SaveState>("idle");
   const [error, setError] = createSignal<string>();

   // The scope is picked once from props, but this page can mount before the
   // first load lands, most often through a #/services deep link. Without this
   // the selection stays on an empty name: the note reads "blocked for every
   // client on ." and apply refuses to send anything, so the sliders look dead.
   createEffect(
      () => {
         const current = scope();
         const named =
            current.kind === "profile"
               ? props.profileNames.includes(current.name)
               : props.clients.some((client) => client.name === current.name);
         if (named) {
            return undefined;
         }
         return current.kind === "profile"
            ? { kind: "profile" as const, name: props.defaultProfile || props.profileNames[0] || "" }
            : { kind: "client" as const, name: props.clients[0]?.name || "" };
      },
      (adopted) => {
         if (adopted?.name) {
            setScope(adopted);
         }
      },
   );

   const windowCrud = createCrud<string>();
   const [winName, setWinName] = createSignal("");
   const [winAction, setWinAction] = createSignal<"block" | "allow">("block");
   const [winSchedule, setWinSchedule] = createSignal("");
   const [winClients, setWinClients] = createSignal<string[]>([]);
   const [winServices, setWinServices] = createSignal<string[]>([]);
   const [winCollapsed, setWinCollapsed] = createSignal<string[]>([]);

   const grouped = createMemo(() => groupServices(props.services, props.serviceGroups));

   const clientProfile = (name: string) =>
      props.clients.find((client) => client.name === name)?.profile ?? "";

   // editableIds is the set this scope's own sliders control.
   const editableIds = createMemo(() => {
      const current = scope();
      return new Set(
         props.services
            .filter((service) =>
               current.kind === "profile" ? service.profiles.includes(current.name) : service.clients.includes(current.name),
            )
            .map((service) => service.id),
      );
   });

   // inheritedIds is what already blocks the viewed client through its profile.
   const inheritedIds = createMemo(() => {
      const current = scope();
      if (current.kind !== "client") {
         return new Set<string>();
      }
      const profile = clientProfile(current.name);
      return new Set(
         props.services.filter((service) => service.profiles.includes(profile)).map((service) => service.id),
      );
   });

   const matches = (service: BlockedService) =>
      search() === "" ||
      service.name.toLowerCase().includes(search().toLowerCase()) ||
      service.id.toLowerCase().includes(search().toLowerCase());

   // cardState is read inside JSX expressions, not destructured beforehand, so
   // every read is tracked and a scope change re-renders the cards.
   const cardState = (service: BlockedService) => {
      const own = editableIds().has(service.id);
      return { own, inherited: !own && inheritedIds().has(service.id) };
   };

   const visibleGroups = createMemo(() =>
      grouped()
         .map((group) => ({ ...group, services: group.services.filter(matches) }))
         .filter((group) => group.services.length > 0),
   );

   function pick(kind: "profile" | "client", name: string) {
      setScope({ kind, name });
      setSaveState("idle");
      setError(undefined);
   }

   async function apply(next: string[]) {
      const current = scope();
      if (!current.name) {
         setError(`no ${current.kind} is selected yet`);
         return;
      }
      setBusy(true);
      setSaveState("saving");
      setError(undefined);
      try {
         await props.onSave(current, next);
         setSaveState("saved");
      } catch (cause) {
         setSaveState("idle");
         setError(String(cause));
      } finally {
         setBusy(false);
      }
   }

   // toggle flips one service in the set this scope owns. The switch's own
   // state decides the direction: checked means the click removes it.
   const toggle = (id: string) => apply(toggled([...editableIds()], id));
   const blockAll = (ids: string[]) => apply([...new Set([...editableIds(), ...ids])]);
   const unblockAll = (ids: string[]) => apply([...editableIds()].filter((id) => !ids.includes(id)));

   async function refreshCatalog() {
      setError(undefined);
      try {
         await props.onRefreshServices();
      } catch (cause) {
         setError(String(cause));
      }
   }

   const inheritedCount = () => [...inheritedIds()].filter((id) => !editableIds().has(id)).length;

   function resetWindow() {
      setWinName("");
      setWinAction("block");
      setWinSchedule("");
      setWinClients([]);
      setWinServices([]);
      setWinCollapsed([]);
   }

   function fillWindow(window: ServiceWindow) {
      setWinName(window.name);
      setWinAction(window.action);
      setWinSchedule(window.schedule);
      setWinClients([...window.clients]);
      setWinServices([...window.services]);
   }

   function openNewWindow() {
      resetWindow();
      windowCrud.openNew();
   }

   function editWindow(window: ServiceWindow) {
      fillWindow(window);
      windowCrud.openEdit(window.name);
   }

   // The name is the key the windows API writes through, so the copy asks for a
   // free one and keeps the schedule, the clients, and the services.
   function duplicateWindow(window: ServiceWindow) {
      fillWindow(window);
      setWinName(duplicateName(window.name, props.windows.map((entry) => entry.name)));
      windowCrud.openDuplicate(window.name);
   }

   async function submitWindow(event: SubmitEvent) {
      event.preventDefault();
      if (!winName().trim()) {
         windowCrud.setError("a window needs a name");
         return;
      }
      if (!winSchedule()) {
         windowCrud.setError("a window needs a schedule");
         return;
      }
      if (winClients().length === 0) {
         windowCrud.setError("a window needs at least one client");
         return;
      }
      if (winServices().length === 0) {
         windowCrud.setError("a window needs at least one service");
         return;
      }
      const input: ServiceWindowInput = {
         action: winAction(),
         schedule: winSchedule(),
         clients: winClients(),
         services: winServices(),
      };
      const key = winName().trim();
      await windowCrud.run(async () => {
         await props.onSaveWindow(key, input);
         resetWindow();
         windowCrud.close();
      });
   }

   async function removeWindow(target: string) {
      await windowCrud.run(async () => {
         await props.onDeleteWindow(target);
         const editor = windowCrud.editor();
         if (editor.mode === "edit" && editor.key === target) {
            resetWindow();
            windowCrud.close();
         }
      });
   }

   function toggleCollapsed(group: string) {
      setWinCollapsed((current) => (current.includes(group) ? current.filter((entry) => entry !== group) : [...current, group]));
   }

   return (
      <div class="view">
         <div class="subbar">
            <div class="seg" role="tablist">
               <button
                  type="button"
                  class={`seg-btn${scope().kind === "profile" ? " active" : ""}`}
                  disabled={busy()}
                  onClick={() => pick("profile", scope().kind === "profile" ? scope().name : props.defaultProfile || props.profileNames[0] || "")}
               >
                  Whole profile
               </button>
               <button
                  type="button"
                  class={`seg-btn${scope().kind === "client" ? " active" : ""}`}
                  disabled={busy()}
                  onClick={() => pick("client", scope().kind === "client" ? scope().name : props.clients[0]?.name || "")}
               >
                  Single client
               </button>
            </div>
            <Show
               when={scope().kind === "client"}
               fallback={
                  <select
                     data-testid="services-scope"
                     value={scope().name}
                     disabled={busy()}
                     onChange={(event) => pick("profile", event.currentTarget.value)}
                  >
                     <For each={props.profileNames}>{(name) => <option value={name}>{name}</option>}</For>
                  </select>
               }
            >
               <select
                  data-testid="services-scope"
                  value={scope().name}
                  disabled={busy()}
                  onChange={(event) => pick("client", event.currentTarget.value)}
               >
                  <For each={props.clients}>{(client) => <option value={client.name}>{client.name}</option>}</For>
               </select>
            </Show>
            <input
               type="search"
               placeholder="Search services…"
               aria-label="Search services"
               data-testid="services-search"
               value={search()}
               onInput={(event) => setSearch(event.currentTarget.value)}
            />
            <div class="blocked-services-actions">
               <button type="button" class="btn-mini" data-testid="services-block-all" disabled={busy()} onClick={() => blockAll(props.services.map((service) => service.id))}>
                  Block all
               </button>
               <button type="button" class="btn-mini" data-testid="services-unblock-all" disabled={busy()} onClick={() => unblockAll(props.services.map((service) => service.id))}>
                  Unblock all
               </button>
            </div>
            <span class="spacer" />
            <SaveStatus state={saveState()} />
            <button type="button" class="btn-mini" data-testid="services-refresh" onClick={() => void refreshCatalog()}>
               Refresh catalog
            </button>
         </div>

         <div class="view-scroll">
            <div class="view-inner" style={{ "max-width": "none" }}>
               <div class="blocked-services-summary">
                  <Show
                     when={scope().kind === "client"}
                     fallback={
                        <p class="muted" data-testid="scope-note" style={{ "font-size": "12px" }}>
                           These services are blocked for every client on <strong>{scope().name}</strong>.
                        </p>
                     }
                  >
                     <p class="muted" data-testid="scope-note" style={{ "font-size": "12px" }}>
                        Blocked for <strong>{scope().name}</strong> only, on top of its profile.{" "}
                        <Show when={inheritedCount() > 0}>
                           {inheritedCount()} more come from the <strong>{clientProfile(scope().name)}</strong> profile and show as
                           locked.
                        </Show>
                     </p>
                  </Show>
               </div>

               <Show when={props.services.length === 0}>
                  <p class="muted">No services in the catalog yet.</p>
               </Show>

               <For each={visibleGroups()}>
                  {(group) => (
                     <div class="service-section">
                        <div class="service-section-head">
                           <h2>{serviceGroupLabel(group.group)}</h2>
                           <span class="service-count">
                              {group.services.filter((service) => editableIds().has(service.id) || inheritedIds().has(service.id)).length}
                              /
                              {group.services.length}
                           </span>
                           <button type="button" class="btn-mini" disabled={busy()} onClick={() => blockAll(group.services.map((service) => service.id))}>
                              Block all
                           </button>
                           <button type="button" class="btn-mini" disabled={busy()} onClick={() => unblockAll(group.services.map((service) => service.id))}>
                              Unblock all
                           </button>
                        </div>
                        <div class="service-grid">
                           <For each={group.services}>
                              {(service) => (
                                 <label
                                    class={`service-card${cardState(service).inherited ? " inherited" : ""}`}
                                    title={cardState(service).inherited ? `Blocked by the ${clientProfile(scope().name)} profile` : ""}
                                 >
                                    <Show when={service.icon_svg} fallback={<span class="service-icon service-icon-fallback">{service.name[0]}</span>}>
                                       <span class="service-icon" innerHTML={service.icon_svg} />
                                    </Show>
                                    <span class="service-name">{service.name}</span>
                                    <Show when={cardState(service).inherited}>
                                       <span class="badge profile">profile</span>
                                    </Show>
                                    <input
                                       type="checkbox"
                                       class="switch"
                                       data-testid={`service-${service.id}`}
                                       checked={cardState(service).own || cardState(service).inherited}
                                       disabled={busy() || cardState(service).inherited}
                                       onChange={() => void toggle(service.id)}
                                    />
                                 </label>
                              )}
                           </For>
                        </div>
                     </div>
                  )}
               </For>

               <section class="sheet" style={{ "margin-top": "22px" }}>
                  <div class="sheet-head">
                     <h2>Time windows</h2>
                     <span class="muted" style={{ "font-size": "11.5px", "margin-right": "12px" }}>
                        A window changes service blocking for its clients while its schedule holds.
                     </span>
                     <button type="button" class="btn-mini" onClick={openNewWindow}>
                        + New window
                     </button>
                  </div>
                  <Show when={windowCrud.error()}>
                     <p class="error" style={{ margin: "10px 16px" }}>
                        {windowCrud.error()}
                     </p>
                  </Show>
                  <Show when={props.windows.length === 0}>
                     <p class="muted" data-testid="windows-empty" style={{ padding: "12px 16px" }}>
                        No windows yet. Always-on toggles above are active around the clock.
                     </p>
                  </Show>
                  <ul style={{ "list-style": "none", padding: "10px 12px", display: "flex", "flex-direction": "column", gap: "8px" }}>
                     <For each={props.windows}>
                        {(window) => (
                           <li
                              data-testid="window-row"
                              style={{
                                 display: "flex",
                                 "align-items": "center",
                                 "justify-content": "space-between",
                                 gap: "12px",
                                 padding: "9px 12px",
                                 border: "1px solid var(--line)",
                                 "border-radius": "12px",
                                 background: "var(--surface-2)",
                              }}
                           >
                              <div style={{ display: "flex", "flex-direction": "column", gap: "2px", "min-width": "0" }}>
                                 <span class="entry-title">
                                    {window.name}
                                    <span class={`badge ${window.action === "allow" ? "allow" : "block"}`} data-testid="window-action">
                                       {window.action}
                                    </span>
                                 </span>
                                 <span class="entry-sub">
                                    {describeSchedule(props.schedules.find((entry) => entry.name === window.schedule))}
                                    {" · "}
                                    {window.clients.join(", ")}
                                    {" · "}
                                    {window.services.length} service{window.services.length === 1 ? "" : "s"}
                                 </span>
                              </div>
                              <CrudActions
                                 testid="window"
                                 label={window.name}
                                 busy={windowCrud.busy()}
                                 onEdit={() => editWindow(window)}
                                 onDuplicate={() => duplicateWindow(window)}
                                 onDelete={() => void removeWindow(window.name)}
                              />
                           </li>
                        )}
                     </For>
                  </ul>
               </section>

               {error() ? <p class="error">{error()}</p> : null}
            </div>
         </div>

         <Drawer
            open={windowCrud.editor().mode !== "closed"}
            title={editorTitle(windowCrud.editor(), "window")}
            onClose={windowCrud.close}
         >
            <form onSubmit={(event) => void submitWindow(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="window-name"
                     value={winName()}
                     placeholder="video-time"
                     disabled={windowCrud.editor().mode === "edit"}
                     onInput={(event) => setWinName(event.currentTarget.value)}
                  />
               </label>
               <label>
                  Schedule
                  <select data-testid="window-schedule" value={winSchedule()} onInput={(event) => setWinSchedule(event.currentTarget.value)}>
                     <option value="">choose a schedule</option>
                     <For each={props.schedules}>{(entry) => <option value={entry.name}>{entry.name}</option>}</For>
                  </select>
               </label>
               <Show when={props.schedules.length === 0}>
                  <p class="muted" style={{ "font-size": "11.5px" }}>
                     Create a schedule first; a window is only active while its schedule holds.
                  </p>
               </Show>
               <div class="window-action-picker">
                  <span>While the schedule holds</span>
                  <div class="seg">
                     <button
                        type="button"
                        class={`seg-btn${winAction() === "block" ? " active" : ""}`}
                        data-testid="window-block"
                        disabled={windowCrud.busy()}
                        onClick={() => setWinAction("block")}
                     >
                        Block
                     </button>
                     <button
                        type="button"
                        class={`seg-btn${winAction() === "allow" ? " active" : ""}`}
                        data-testid="window-allow"
                        disabled={windowCrud.busy()}
                        onClick={() => setWinAction("allow")}
                     >
                        Allow
                     </button>
                  </div>
               </div>

               <fieldset class="services" data-testid="window-clients" style={{ width: "100%" }}>
                  <legend>Clients</legend>
                  <Show when={props.clients.length === 0}>
                     <p class="muted">No clients yet.</p>
                  </Show>
                  <For each={props.clients}>
                     {(client) => (
                        <label class="toggle">
                           <input
                              type="checkbox"
                              data-testid={`window-client-${client.name}`}
                              checked={winClients().includes(client.name)}
                              disabled={windowCrud.busy()}
                              onChange={() => setWinClients((current) => toggled(current, client.name))}
                           />
                           {client.name}
                        </label>
                     )}
                  </For>
               </fieldset>

               <fieldset class="services" data-testid="window-services" style={{ width: "100%" }}>
                  <legend>Services</legend>
                  <Show when={props.services.length === 0}>
                     <p class="muted">No services in the catalog yet.</p>
                  </Show>
                  <div style={{ display: "flex", "flex-direction": "column", gap: "10px", width: "100%" }}>
                     <For each={groupServices(props.services, props.serviceGroups)}>
                        {(group) => {
                           const key = group.group || "other";
                           const ids = group.services.map((service) => service.id);
                           const state = () => groupState(ids, winServices());
                           const count = () => ids.filter((id) => winServices().includes(id)).length;
                           const open = () => !winCollapsed().includes(key);
                           return (
                              <div class="window-group">
                                 <div class="group-head">
                                    <label class="toggle">
                                       <input
                                          type="checkbox"
                                          data-testid={`window-group-${key}`}
                                          checked={state() === "all"}
                                          disabled={windowCrud.busy()}
                                          onChange={() => setWinServices((current) => toggledGroup(current, ids))}
                                       />
                                       <strong>{serviceGroupLabel(group.group)}</strong>
                                    </label>
                                    <span class="muted">
                                       {count()}/{ids.length}
                                    </span>
                                    <button type="button" class="btn-mini" data-testid={`window-expand-${key}`} onClick={() => toggleCollapsed(key)}>
                                       {open() ? "Hide" : "Show"}
                                    </button>
                                 </div>
                                 <Show when={open()}>
                                    <div class="group-services">
                                       <For each={group.services}>
                                          {(service) => (
                                             <label class="toggle">
                                                <input
                                                   type="checkbox"
                                                   data-testid={`window-service-${service.id}`}
                                                   checked={winServices().includes(service.id)}
                                                   disabled={windowCrud.busy()}
                                                   onChange={() => setWinServices((current) => toggled(current, service.id))}
                                                />
                                                {service.name}
                                             </label>
                                          )}
                                       </For>
                                    </div>
                                 </Show>
                              </div>
                           );
                        }}
                     </For>
                  </div>
               </fieldset>

               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <button type="submit" class="btn" data-testid="window-save" disabled={windowCrud.busy()}>
                     Save window
                  </button>
               </div>
            </form>
         </Drawer>
      </div>
   );
}
