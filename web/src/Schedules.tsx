import { createSignal, For, Show } from "solid-js";
import type { Schedule, ScheduleWindow } from "./api";
import DataTable, { type Column } from "./DataTable";
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
  const [name, setName] = createSignal("");
  const [priority, setPriority] = createSignal("1");
  const [windows, setWindows] = createSignal<ScheduleWindow[]>([]);
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);
  const [drawer, setDrawer] = createSignal(false);

  const namedByRules = () => new Set(props.rules.map((rule) => rule.schedule).filter(Boolean));

  function blankWindow(): ScheduleWindow {
    return { days: [1, 2, 3, 4, 5], start: "21:00", end: "07:00" };
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name()) {
      setError("a schedule needs a name");
      return;
    }
    if (windows().length === 0) {
      setError("a schedule needs at least one window");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(name().trim(), { priority: Number(priority()) || 1, windows: windows() });
      setName("");
      setPriority("1");
      setWindows([]);
      setDrawer(false);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
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
    setError(undefined);
    try {
      await props.onDelete(target);
    } catch (cause) {
      setError(String(cause));
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
        <button type="button" class="btn-mini" data-testid="schedule-delete" onClick={() => void remove(schedule.name)}>
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
            {props.schedules.length} schedules
          </h2>
          <Show when={namedByRules().size > 0}>
            <span class="muted" style={{ "font-size": "11.5px" }}>a schedule a rule still names cannot be deleted</span>
          </Show>
          <button
            type="button"
            class="btn"
            data-testid="schedule-add"
            onClick={() => {
              setName("");
              setPriority("1");
              setWindows([]);
              setError(undefined);
              setDrawer(true);
            }}
          >
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

      <Drawer open={drawer()} title="New schedule" onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
          <label>
            Name
            <input
              data-testid="schedule-name"
              value={name()}
              placeholder="night"
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

          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="schedule-save" disabled={busy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
