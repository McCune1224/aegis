import { createSignal, For, Show } from "solid-js";
import type { Schedule, ScheduleWindow } from "./api";
import CrudActions from "./CrudActions";
import SaveStatus from "./SaveStatus";
import DataTable, { type Column } from "./DataTable";
import { createCrud, duplicateName, editorTitle } from "./crud";
import Drawer from "./Drawer";

const DAY_LABELS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];

type Props = {
   schedules: Schedule[];
   rules: { schedule?: string }[];
   onSave: (name: string, input: { priority: number; windows: ScheduleWindow[] }) => Promise<void>;
   onDelete: (name: string) => Promise<void>;
};

function describeWindow(window: ScheduleWindow): string {
   const days = window.days.length === 7 ? "every day" : window.days.map((day) => DAY_LABELS[day] ?? "?").join(" ");
   return `${days} ${window.start} to ${window.end}`;
}

function describe(schedule: Schedule): string {
   return schedule.windows.map(describeWindow).join("; ");
}

export default function Schedules(props: Props) {
   const crud = createCrud<string>();
   const [name, setName] = createSignal("");
   const [priority, setPriority] = createSignal("1");
   const [windows, setWindows] = createSignal<ScheduleWindow[]>([]);
   const [rowError, setRowError] = createSignal<string>();

   const namedByRules = () => new Set(props.rules.map((rule) => rule.schedule).filter(Boolean));

   function blankWindow(): ScheduleWindow {
      return { days: [1, 2, 3, 4, 5], start: "21:00", end: "07:00" };
   }

   function clear() {
      setName("");
      setPriority("1");
      setWindows([]);
   }

   function fill(schedule: Schedule) {
      setName(schedule.name);
      setPriority(String(schedule.priority));
      setWindows(schedule.windows.map((window) => ({ ...window, days: [...window.days] })));
   }

   function openNew() {
      clear();
      crud.openNew();
   }

   function edit(schedule: Schedule) {
      fill(schedule);
      crud.openEdit(schedule.name);
   }

   // A copy keeps the windows and the priority and asks for a new name, because
   // the name is the key the schedules API writes through.
   function duplicate(schedule: Schedule) {
      fill(schedule);
      setName(duplicateName(schedule.name, props.schedules.map((entry) => entry.name)));
      crud.openDuplicate(schedule.name);
   }

   async function submit(event: SubmitEvent) {
      event.preventDefault();
      if (!name()) {
         crud.setError("a schedule needs a name");
         return;
      }
      if (windows().length === 0) {
         crud.setError("a schedule needs at least one window");
         return;
      }
      const input = { priority: Number(priority()) || 1, windows: windows() };
      const key = name().trim();
      await crud.run(async () => {
         await props.onSave(key, input);
         clear();
         crud.close();
      });
   }

   function toggleDay(index: number, day: number) {
      setWindows((current) =>
         current.map((window, position) => {
            if (position !== index) {
               return window;
            }
            const days = window.days.includes(day) ? window.days.filter((d) => d !== day) : [...window.days, day].sort();
            return { ...window, days };
         }),
      );
   }

   async function remove(target: string) {
      setRowError(undefined);
      try {
         await props.onDelete(target);
      } catch (cause) {
         setRowError(String(cause));
      }
   }

   const columns: Column<Schedule>[] = [
      {
         key: "name",
         label: "Schedule",
         sortable: true,
         value: (schedule) => schedule.name,
         render: (schedule) => <span class="mono">{schedule.name}</span>,
      },
      {
         key: "priority",
         label: "Priority",
         sortable: true,
         value: (schedule) => schedule.priority,
         render: (schedule) => <span class="badge kind">p{schedule.priority}</span>,
      },
      {
         key: "windows",
         label: "Windows",
         value: (schedule) => describe(schedule),
         render: (schedule) => <span class="muted" style={{ "font-size": "11.5px" }}>{describe(schedule)}</span>,
      },
      {
         key: "actions",
         label: "",
         value: () => "",
         render: (schedule) => (
            <CrudActions
               testid="schedule"
               label={schedule.name}
               busy={crud.busy()}
               onEdit={() => edit(schedule)}
               onDuplicate={() => duplicate(schedule)}
               onDelete={() => void remove(schedule.name)}
            />
         ),
      },
   ];

   return (
      <>
         <div class="view">
            <div class="subbar">
               <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
                  {props.schedules.length} schedules
               </h2>
               <Show when={namedByRules().size > 0}>
                  <span class="muted" style={{ "font-size": "11.5px" }}>a schedule a rule still names cannot be deleted</span>
               </Show>
               <Show when={rowError()}>
                  <p class="alert-line error-line" role="alert">
                     {rowError()}
                  </p>
               </Show>
               <button type="button" class="btn" data-testid="schedule-add" onClick={openNew}>
                  + New schedule
               </button>
            </div>
            <div style={{ flex: "1", "min-height": "0", overflow: "auto" }}>
               <DataTable
                  columns={columns}
                  rows={props.schedules}
                  rowKey={(schedule) => schedule.name}
                  testid="schedule-rows"
                  rowTestid={() => "schedule-row"}
                  empty="no schedules yet"
               />
            </div>
         </div>

         <Drawer
            open={crud.editor().mode !== "closed"}
            title={editorTitle(crud.editor(), "schedule")}
            onClose={crud.close}
         >
            <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="schedule-name"
                     value={name()}
                     placeholder="night"
                     disabled={crud.editor().mode === "edit"}
                     onInput={(event) => setName(event.currentTarget.value)}
                  />
               </label>
               <label>
                  Priority
                  <input
                     data-testid="schedule-priority"
                     type="number"
                     value={priority()}
                     onInput={(event) => setPriority(event.currentTarget.value)}
                  />
               </label>

               <div class="windows-editor">
                  <For each={windows()}>
                     {(window, index) => (
                        <div class="window-editor" data-testid="schedule-window">
                           <div class="day-picker">
                              <For each={DAY_LABELS}>
                                 {(label, day) => (
                                    <label class="day-option">
                                       <input
                                          type="checkbox"
                                          checked={window.days.includes(day())}
                                          onChange={() => toggleDay(index(), day())}
                                       />
                                       {label}
                                    </label>
                                 )}
                              </For>
                           </div>
                           <input
                              type="time"
                              value={window.start}
                              onInput={(event) =>
                                 setWindows((current) =>
                                    current.map((entry, position) =>
                                       position === index() ? { ...entry, start: event.currentTarget.value } : entry,
                                    ),
                                 )
                              }
                           />
                           <input
                              type="time"
                              value={window.end}
                              onInput={(event) =>
                                 setWindows((current) =>
                                    current.map((entry, position) =>
                                       position === index() ? { ...entry, end: event.currentTarget.value } : entry,
                                    ),
                                 )
                              }
                           />
                           <button
                              type="button"
                              class="btn-mini"
                              onClick={() => setWindows((current) => current.filter((_, position) => position !== index()))}
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
                     onClick={() => setWindows((current) => [...current, blankWindow()])}
                  >
                     Add window
                  </button>
               </div>

               {crud.error() ? <p class="error">{crud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <SaveStatus state={crud.saveState()} />
                  <button type="submit" class="btn" data-testid="schedule-save" disabled={crud.busy()}>
                     Save
                  </button>
               </div>
            </form>
         </Drawer>
      </>
   );
}
