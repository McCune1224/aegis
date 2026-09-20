import { createSignal, For, Show } from "solid-js";
import type { BlockedService, Profile, ProfileInput } from "./api";
import { groupServices, toggled } from "./services";

const modes = ["nxdomain", "null-address", "custom-address", "refused"];

type Props = {
  profiles: Profile[];
  defaultProfile: string;
  services: BlockedService[];
  serviceGroups: string[];
  onSave: (name: string, input: ProfileInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
  onSetDefault: (name: string) => Promise<void>;
  onSaveServices: (name: string, services: string[]) => Promise<void>;
  onRefreshServices: () => Promise<void>;
};

export default function Profiles(props: Props) {
  const [name, setName] = createSignal("");
  const [parent, setParent] = createSignal("");
  const [mode, setMode] = createSignal("");
  const [custom, setCustom] = createSignal("");
  const [editing, setEditing] = createSignal<string>();
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setName("");
    setParent("");
    setMode("");
    setCustom("");
    setEditing(undefined);
  }

  function edit(profile: Profile) {
    setEditing(profile.name);
    setName(profile.name);
    setParent(profile.extends ?? "");
    setMode(profile.mode ?? "");
    setCustom(profile.custom ?? "");
    setError(undefined);
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name()) {
      setError("a profile needs a name");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(name(), { extends: parent(), mode: mode(), custom: custom() });
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

  async function makeDefault(target: string) {
    setError(undefined);
    try {
      await props.onSetDefault(target);
    } catch (cause) {
      setError(String(cause));
    }
  }

  function enabledServices(): string[] {
    const profile = editing();
    if (!profile) {
      return [];
    }
    return props.services.filter((service) => service.profiles.includes(profile)).map((service) => service.id);
  }

  async function toggleService(id: string) {
    const profile = editing();
    if (!profile) {
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSaveServices(profile, toggled(enabledServices(), id));
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
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
        <For each={props.profiles}>
          {(profile) => (
            <li data-testid="profile-row">
              <div>
                <strong>{profile.name}</strong>
                <span class="muted">
                  {profile.mode ?? "inherited"}
                  {profile.extends ? ` from ${profile.extends}` : ""}
                  {profile.custom ? ` ${profile.custom}` : ""}
                </span>
              </div>
              <div class="row-actions">
                {profile.name === props.defaultProfile ? (
                  <span class="badge" data-testid="profile-default">
                    default
                  </span>
                ) : (
                  <button type="button" onClick={() => void makeDefault(profile.name)}>
                    Make default
                  </button>
                )}
                <button type="button" onClick={() => edit(profile)}>
                  Edit
                </button>
                <button type="button" onClick={() => void remove(profile.name)}>
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>{editing() ? `Edit ${editing()}` : "New profile"}</h2>
        <label>
          Name
          <input
            data-testid="profile-name"
            value={name()}
            disabled={editing() !== undefined}
            onInput={(event) => setName(event.currentTarget.value)}
          />
        </label>
        <label>
          Parent
          <select
            data-testid="profile-extends"
            value={parent()}
            onInput={(event) => setParent(event.currentTarget.value)}
          >
            <option value="">none</option>
            <For each={props.profiles}>
              {(profile) => (
                <Show when={profile.name !== editing()}>
                  <option value={profile.name}>{profile.name}</option>
                </Show>
              )}
            </For>
          </select>
        </label>
        <label>
          Mode
          <select
            data-testid="profile-mode"
            value={mode()}
            onInput={(event) => setMode(event.currentTarget.value)}
          >
            <option value="">inherit</option>
            <For each={modes}>{(value) => <option value={value}>{value}</option>}</For>
          </select>
        </label>
        <Show when={mode() === "custom-address"}>
          <label>
            Custom address
            <input
              data-testid="profile-custom"
              value={custom()}
              onInput={(event) => setCustom(event.currentTarget.value)}
            />
          </label>
        </Show>
        <div class="row-actions">
          <button type="submit" data-testid="profile-save" disabled={busy()}>
            Save
          </button>
          <Show when={editing()}>
            <button type="button" onClick={reset}>
              Cancel
            </button>
          </Show>
        </div>
        <Show when={editing()}>
          <fieldset class="services" data-testid="profile-services">
            <legend>Blocked services</legend>
            <Show when={props.services.length === 0}>
              <p class="muted">No services in the catalog yet.</p>
            </Show>
            <For each={groupServices(props.services, props.serviceGroups)}>
              {(group) => (
                <div class="service-group">
                  <h3>{group.group || "Other"}</h3>
                  <For each={group.services}>
                    {(service) => (
                      <label class="toggle">
                        <input
                          type="checkbox"
                          data-testid={`service-${service.id}`}
                          checked={enabledServices().includes(service.id)}
                          disabled={busy()}
                          onChange={() => void toggleService(service.id)}
                        />
                        {service.name}
                      </label>
                    )}
                  </For>
                </div>
              )}
            </For>
            <button type="button" data-testid="services-refresh" onClick={() => void refreshCatalog()}>
              Refresh catalog
            </button>
          </fieldset>
        </Show>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
    </section>
  );
}
