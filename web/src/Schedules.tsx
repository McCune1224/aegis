import { createSignal, For, Show } from "solid-js";
import type { Schedule, ScheduleWindow } from "./api";

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

export default function Schedules(props: Props) {
  const [name, setName] = createSignal("");
  const [priority, setPriority] = createSignal("1");
  const [windows, setWindows] = createSignal<ScheduleWindow[]>([]);
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

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

  async function remove(name: string) {
    setError(undefined);
    try {
      await props.onDelete(name);
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <section class="panel">
      <ul>
        <For each={props.schedules}>
          {(schedule) => (
            <li data-testid="schedule-row">
              <div>
                <strong>{schedule.name}</strong>
                <span class="muted">priority {schedule.priority}</span>
                <For each={schedule.windows}>
                  {(window) => <span class="selectors">{describeWindow(window)}</span>}
                </For>
              </div>
              <div class="row-actions">
                <button
                  type="button"
                  data-testid="schedule-delete"
                  onClick={() => void remove(schedule.name)}
                >
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>New schedule</h2>
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
                  onClick={() => setWindows((current) => current.filter((_, position) => position !== index()))}
                >
                  Remove
                </button>
              </div>
            )}
          </For>
          <button
            type="button"
            data-testid="schedule-add-window"
            onClick={() => setWindows((current) => [...current, blankWindow()])}
          >
            Add window
          </button>
        </div>

        <div class="row-actions">
          <button type="submit" data-testid="schedule-save" disabled={busy()}>
            Save
          </button>
        </div>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
      <Show when={namedByRules().size > 0}>
        <p class="muted">A schedule a rule still names cannot be deleted.</p>
      </Show>
    </section>
  );
}
