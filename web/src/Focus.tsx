import { createSignal, For, Show } from "solid-js";
import type { BlockedService, Client, FocusWindow, FocusWindowInput, Schedule } from "./api";
import { groupServices, groupState, toggled, toggledGroup } from "./services";

const DAY_LABELS = ["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"];

type Props = {
  windows: FocusWindow[];
  schedules: Schedule[];
  clients: Client[];
  services: BlockedService[];
  serviceGroups: string[];
  onSave: (name: string, input: FocusWindowInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
  onRefreshServices: () => Promise<void>;
};

function describeSchedule(schedule: Schedule | undefined): string {
  if (!schedule) {
    return "";
  }
  return schedule.windows
    .map((window) => {
      const days =
        window.days.length === 7 ? "every day" : window.days.map((day) => DAY_LABELS[day] ?? "?").join(" ");
      return `${days} ${window.start}-${window.end}`;
    })
    .join("; ");
}

export default function Focus(props: Props) {
  const [name, setName] = createSignal("");
  const [schedule, setSchedule] = createSignal("");
  const [selectedClients, setSelectedClients] = createSignal<string[]>([]);
  const [selectedServices, setSelectedServices] = createSignal<string[]>([]);
  const [collapsed, setCollapsed] = createSignal<string[]>([]);
  const [editing, setEditing] = createSignal<string>();
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  const scheduleByName = (target: string) => props.schedules.find((entry) => entry.name === target);

  function reset() {
    setName("");
    setSchedule("");
    setSelectedClients([]);
    setSelectedServices([]);
    setCollapsed([]);
    setEditing(undefined);
  }

  function edit(window: FocusWindow) {
    setEditing(window.name);
    setName(window.name);
    setSchedule(window.schedule);
    setSelectedClients([...window.clients]);
    setSelectedServices([...window.services]);
    setError(undefined);
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name().trim()) {
      setError("a focus window needs a name");
      return;
    }
    if (!schedule()) {
      setError("a focus window needs a schedule");
      return;
    }
    if (selectedClients().length === 0) {
      setError("a focus window needs at least one client");
      return;
    }
    if (selectedServices().length === 0) {
      setError("a focus window needs at least one service");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(name().trim(), {
        schedule: schedule(),
        clients: selectedClients(),
        services: selectedServices(),
      });
      reset();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function remove(target: string) {
    setError(undefined);
    try {
      await props.onDelete(target);
      if (editing() === target) {
        reset();
      }
    } catch (cause) {
      setError(String(cause));
    }
  }

  function toggleCollapsed(group: string) {
    setCollapsed((current) =>
      current.includes(group) ? current.filter((entry) => entry !== group) : [...current, group],
    );
  }

  async function refreshCatalog() {
    setError(undefined);
    try {
      await props.onRefreshServices();
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <section class="panel">
      <ul>
        <For each={props.windows}>
          {(window) => (
            <li data-testid="focus-row">
              <div>
                <strong>{window.name}</strong>
                <span class="muted">{describeSchedule(scheduleByName(window.schedule))}</span>
                <span class="selectors">{window.clients.join(", ")}</span>
                <span class="selectors">
                  {window.services.length} service{window.services.length === 1 ? "" : "s"}
                </span>
              </div>
              <div class="row-actions">
                <button type="button" onClick={() => edit(window)}>
                  Edit
                </button>
                <button
                  type="button"
                  class="btn-danger"
                  data-testid="focus-delete"
                  onClick={() => void remove(window.name)}
                >
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>{editing() ? `Edit ${editing()}` : "New focus window"}</h2>
        <label>
          Name
          <input
            data-testid="focus-name"
            value={name()}
            placeholder="school-nights"
            disabled={editing() !== undefined}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </label>

        <label>
          Schedule
          <select
            data-testid="focus-schedule"
            value={schedule()}
            onInput={(event) => setSchedule(event.currentTarget.value)}
          >
            <option value="">choose a schedule</option>
            <For each={props.schedules}>{(entry) => <option value={entry.name}>{entry.name}</option>}</For>
          </select>
        </label>
        <Show when={props.schedules.length === 0}>
          <p class="muted">Create a schedule first; a focus window is only active while its schedule holds.</p>
        </Show>

        <fieldset class="services" data-testid="focus-clients">
          <legend>Clients</legend>
          <Show when={props.clients.length === 0}>
            <p class="muted">No clients yet.</p>
          </Show>
          <For each={props.clients}>
            {(client) => (
              <label class="toggle">
                <input
                  type="checkbox"
                  data-testid={`focus-client-${client.name}`}
                  checked={selectedClients().includes(client.name)}
                  disabled={busy()}
                  onChange={() =>
                    setSelectedClients((current) => toggled(current, client.name))
                  }
                />
                {client.name}
              </label>
            )}
          </For>
        </fieldset>

        <fieldset class="services" data-testid="focus-services">
          <legend>Block while the window holds</legend>
          <Show when={props.services.length === 0}>
            <p class="muted">No services in the catalog yet.</p>
          </Show>
          <For each={groupServices(props.services, props.serviceGroups)}>
            {(group) => {
              const key = group.group || "other";
              const ids = group.services.map((service) => service.id);
              const state = () => groupState(ids, selectedServices());
              const count = () => ids.filter((id) => selectedServices().includes(id)).length;
              const open = () => !collapsed().includes(key);
              return (
                <div class="focus-group">
                  <div class="group-head">
                    <label class="toggle">
                      <input
                        type="checkbox"
                        data-testid={`focus-group-${key}`}
                        checked={state() === "all"}
                        disabled={busy()}
                        onChange={() => setSelectedServices((current) => toggledGroup(current, ids))}
                      />
                      <strong>{group.group || "Other"}</strong>
                    </label>
                    <span class="muted">
                      {count()}/{ids.length}
                    </span>
                    <button
                      type="button"
                      class="btn-ghost"
                      data-testid={`focus-expand-${key}`}
                      onClick={() => toggleCollapsed(key)}
                    >
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
                              data-testid={`focus-service-${service.id}`}
                              checked={selectedServices().includes(service.id)}
                              disabled={busy()}
                              onChange={() =>
                                setSelectedServices((current) => toggled(current, service.id))
                              }
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
          <button type="button" data-testid="focus-refresh" onClick={() => void refreshCatalog()}>
            Refresh catalog
          </button>
        </fieldset>

        <div class="row-actions">
          <button type="submit" data-testid="focus-save" disabled={busy()}>
            Save
          </button>
          <Show when={editing()}>
            <button type="button" onClick={reset}>
              Cancel
            </button>
          </Show>
        </div>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
    </section>
  );
}
