import { createMemo, createSignal, createEffect, For, Show } from "solid-js";
import type {
   BlockedService,
   Client,
   Rule,
   Schedule,
   ScheduleInput,
   ScheduleWindow,
   ServiceWindow,
   ServiceWindowInput,
} from "./api";
import CrudActions from "./CrudActions";
import { createCrud, duplicateName, editorTitle, type SaveState } from "./crud";
import { groupServices, groupState, serviceGroupLabel, toggled, toggledGroup } from "./services";
import { describeSchedule, scheduleUsage } from "./schedules";
import Drawer from "./Drawer";
import SaveStatus from "./SaveStatus";

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
   // rules are the custom rules a schedule can also gate, which is what the
   // usage line and a refused delete name.
   rules: Rule[];
   onSave: (scope: ServiceScope, services: string[]) => Promise<void>;
   onRefreshServices: () => Promise<void>;
   onSaveWindow: (name: string, input: ServiceWindowInput) => Promise<void>;
   onDeleteWindow: (name: string) => Promise<void>;
   onSaveSchedule: (name: string, input: ScheduleInput) => Promise<void>;
   onDeleteSchedule: (name: string) => Promise<void>;
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

   // One drawer edits either record kind, because a window is nothing without
   // its schedule and the two forms live on one page. A schedule opened from
   // the window form remembers that, so closing it returns to the window being
   // written instead of dropping it.
   const scheduleCrud = createCrud<string>();
   const [drawerMode, setDrawerMode] = createSignal<"window" | "schedule">();
   const [fromWindowForm, setFromWindowForm] = createSignal(false);
   const [schedName, setSchedName] = createSignal("");
   const [schedPriority, setSchedPriority] = createSignal("1");
   const [schedWindows, setSchedWindows] = createSignal<ScheduleWindow[]>([]);

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
      setFromWindowForm(false);
      setDrawerMode("window");
      windowCrud.openNew();
   }

   function editWindow(window: ServiceWindow) {
      fillWindow(window);
      setFromWindowForm(false);
      setDrawerMode("window");
      windowCrud.openEdit(window.name);
   }

   // The name is the key the windows API writes through, so the copy asks for a
   // free one and keeps the schedule, the clients, and the services.
   function duplicateWindow(window: ServiceWindow) {
      fillWindow(window);
      setWinName(duplicateName(window.name, props.windows.map((entry) => entry.name)));
      setFromWindowForm(false);
      setDrawerMode("window");
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
         setDrawerMode(undefined);
      });
   }

   async function removeWindow(target: string) {
      await windowCrud.run(async () => {
         await props.onDeleteWindow(target);
         const editor = windowCrud.editor();
         if (editor.mode === "edit" && editor.key === target) {
            resetWindow();
            windowCrud.close();
            setDrawerMode(undefined);
         }
      });
   }

   function resetSchedule() {
      setSchedName("");
      setSchedPriority("1");
      setSchedWindows([]);
   }

   function fillSchedule(schedule: Schedule) {
      setSchedName(schedule.name);
      setSchedPriority(String(schedule.priority));
      setSchedWindows(schedule.windows.map((window) => ({ ...window, days: [...window.days] })));
   }

   // blankScheduleWindow is the weeknights span, the shape an operator reaches
   // for first; the editor takes it from there.
   function blankScheduleWindow(): ScheduleWindow {
      return { days: [1, 2, 3, 4, 5], start: "21:00", end: "07:00" };
   }

   function toggleScheduleDay(index: number, day: number) {
      setSchedWindows((current) =>
         current.map((window, position) => {
            if (position !== index) {
               return window;
            }
            const days = window.days.includes(day) ? window.days.filter((entry) => entry !== day) : [...window.days, day].sort();
            return { ...window, days };
         }),
      );
   }

   function setScheduleWindowAt(index: number, patch: { start?: string; end?: string }) {
      setSchedWindows((current) =>
         current.map((window, position) => (position === index ? { ...window, ...patch } : window)),
      );
   }

   // openNewSchedule takes the flag that says the window form asked for it, so
   // closing the schedule editor returns to the window instead of leaving the
   // operator with nothing on screen.
   function openNewSchedule(fromForm: boolean) {
      resetSchedule();
      setFromWindowForm(fromForm);
      setDrawerMode("schedule");
      scheduleCrud.openNew();
   }

   function editSchedule(schedule: Schedule) {
      fillSchedule(schedule);
      setFromWindowForm(false);
      setDrawerMode("schedule");
      scheduleCrud.openEdit(schedule.name);
   }

   // The name is the key the schedules API writes through, so the copy asks for
   // a free one and keeps the priority and the windows.
   function duplicateSchedule(schedule: Schedule) {
      fillSchedule(schedule);
      setSchedName(duplicateName(schedule.name, props.schedules.map((entry) => entry.name)));
      setFromWindowForm(false);
      setDrawerMode("schedule");
      scheduleCrud.openDuplicate(schedule.name);
   }

   function backToWindowForm(): boolean {
      return drawerMode() === "schedule" && fromWindowForm() && windowCrud.editor().mode !== "closed";
   }

   function closeDrawer() {
      const back = backToWindowForm();
      scheduleCrud.close();
      setFromWindowForm(false);
      if (back) {
         setDrawerMode("window");
         return;
      }
      windowCrud.close();
      setDrawerMode(undefined);
   }

   async function submitSchedule(event: SubmitEvent) {
      event.preventDefault();
      const key = schedName().trim();
      if (!key) {
         scheduleCrud.setError("a schedule needs a name");
         return;
      }
      if (schedWindows().length === 0) {
         scheduleCrud.setError("a schedule needs at least one window");
         return;
      }
      const input: ScheduleInput = { priority: Number(schedPriority()) || 1, windows: schedWindows() };
      const back = backToWindowForm();
      await scheduleCrud.run(async () => {
         await props.onSaveSchedule(key, input);
         if (back && !winSchedule()) {
            setWinSchedule(key);
         }
         resetSchedule();
         scheduleCrud.close();
         setFromWindowForm(false);
         setDrawerMode(back ? "window" : undefined);
      });
   }

   async function removeSchedule(target: string) {
      // The editor state is read before the request, because a continuation
      // that reads a signal after the await runs outside any tracking scope.
      const editor = scheduleCrud.editor();
      const back = backToWindowForm();
      await scheduleCrud.run(async () => {
         await props.onDeleteSchedule(target);
         if (editor.mode === "edit" && editor.key === target) {
            resetSchedule();
            scheduleCrud.close();
            setDrawerMode(back ? "window" : undefined);
         }
      });
   }

   // usageLine reads why a schedule matters before an operator deletes it: the
   // windows and the custom rules that still name it.
   function usageLine(name: string): string {
      const used = scheduleUsage(name, props.windows, props.rules);
      const parts: string[] = [];
      if (used.windows > 0) {
         parts.push(`${used.windows} window${used.windows === 1 ? "" : "s"}`);
      }
      if (used.rules > 0) {
         parts.push(`${used.rules} rule${used.rules === 1 ? "" : "s"}`);
      }
      return parts.length > 0 ? `named by ${parts.join(" and ")}` : "not named by anything yet";
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
                     <button type="button" class="btn-mini" data-testid="window-new" onClick={openNewWindow}>
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

               <section class="sheet" style={{ "margin-top": "22px" }}>
                  <div class="sheet-head">
                     <h2>Schedules</h2>
                     <span class="muted" style={{ "font-size": "11.5px", "margin-right": "12px" }}>
                        A schedule is the clock a window or a rule reads, and it applies only while it covers the query minute.
                     </span>
                     <button type="button" class="btn-mini" data-testid="schedule-add" onClick={() => openNewSchedule(false)}>
                        + New schedule
                     </button>
                  </div>
                  <Show when={scheduleCrud.error()}>
                     <p class="error" data-testid="schedule-error" style={{ margin: "10px 16px" }}>
                        {scheduleCrud.error()}
                     </p>
                  </Show>
                  <Show when={props.schedules.length === 0}>
                     <p class="muted" data-testid="schedules-empty" style={{ padding: "12px 16px" }}>
                        No schedules yet. A window needs one to say when it holds.
                     </p>
                  </Show>
                  <ul style={{ "list-style": "none", padding: "10px 12px", display: "flex", "flex-direction": "column", gap: "8px" }}>
                     <For each={props.schedules}>
                        {(schedule) => (
                           <li
                              data-testid="schedule-row"
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
                                    {schedule.name}
                                    <span class="badge kind" data-testid="schedule-priority-badge">
                                       p{schedule.priority}
                                    </span>
                                 </span>
                                 <span class="entry-sub">
                                    {describeSchedule(schedule)}
                                    {" · "}
                                    {usageLine(schedule.name)}
                                 </span>
                              </div>
                              <CrudActions
                                 testid="schedule"
                                 label={schedule.name}
                                 busy={scheduleCrud.busy()}
                                 onEdit={() => editSchedule(schedule)}
                                 onDuplicate={() => duplicateSchedule(schedule)}
                                 onDelete={() => void removeSchedule(schedule.name)}
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
            open={drawerMode() === "window"}
            title={editorTitle(windowCrud.editor(), "window")}
            onClose={closeDrawer}
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
               <div class="row-actions" style={{ "justify-content": "space-between", "align-items": "center" }}>
                  <span class="muted" style={{ "font-size": "11.5px" }}>
                     A window holds only while its schedule covers the query minute.
                  </span>
                  <button type="button" class="btn-ghost" data-testid="window-new-schedule" onClick={() => openNewSchedule(true)}>
                     + New schedule
                  </button>
               </div>
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

         <Drawer
            open={drawerMode() === "schedule"}
            title={editorTitle(scheduleCrud.editor(), "schedule")}
            onClose={closeDrawer}
         >
            <form onSubmit={(event) => void submitSchedule(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="schedule-name"
                     value={schedName()}
                     placeholder="night"
                     disabled={scheduleCrud.editor().mode === "edit"}
                     onInput={(event) => setSchedName(event.currentTarget.value)}
                  />
               </label>
               <label>
                  Priority
                  <input
                     data-testid="schedule-priority"
                     type="number"
                     value={schedPriority()}
                     onInput={(event) => setSchedPriority(event.currentTarget.value)}
                  />
               </label>
               <p class="muted" style={{ "font-size": "11.5px" }}>
                  Priority breaks the tie when two schedules cover the same minute; a higher number wins.
               </p>

               <div class="windows-editor">
                  <For each={schedWindows()}>
                     {(entry, index) => (
                        <div class="window-editor" data-testid="schedule-window">
                           <div class="day-picker">
                              <For each={["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"]}>
                                 {(label, day) => (
                                    <label class="day-option">
                                       <input
                                          type="checkbox"
                                          checked={entry.days.includes(day())}
                                          onChange={() => toggleScheduleDay(index(), day())}
                                       />
                                       {label}
                                    </label>
                                 )}
                              </For>
                           </div>
                           <input
                              type="time"
                              value={entry.start}
                              onInput={(event) => setScheduleWindowAt(index(), { start: event.currentTarget.value })}
                           />
                           <input
                              type="time"
                              value={entry.end}
                              onInput={(event) => setScheduleWindowAt(index(), { end: event.currentTarget.value })}
                           />
                           <button
                              type="button"
                              class="btn-mini"
                              onClick={() => setSchedWindows((current) => current.filter((_, position) => position !== index()))}
                           >
                              Remove
                           </button>
                        </div>
                     )}
                  </For>
                  <button
                     type="button"
                     class="btn-ghost"
                     data-testid="schedule-add-window"
                     onClick={() => setSchedWindows((current) => [...current, blankScheduleWindow()])}
                  >
                     Add window
                  </button>
               </div>

               <Show when={scheduleCrud.error()}>
                  <p class="error" data-testid="schedule-form-error">
                     {scheduleCrud.error()}
                  </p>
               </Show>
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <SaveStatus state={scheduleCrud.saveState()} />
                  <button type="submit" class="btn" data-testid="schedule-save" disabled={scheduleCrud.busy()}>
                     Save schedule
                  </button>
               </div>
            </form>
         </Drawer>
      </div>
   );
}
