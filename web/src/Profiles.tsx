import { createSignal, For, Show } from "solid-js";
import type { Profile, ProfileInput } from "./api";

const modes = ["nxdomain", "null-address", "custom-address", "refused"];

type Props = {
  profiles: Profile[];
  onSave: (name: string, input: ProfileInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
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

  return (
    <section>
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
        {error() ? <p class="error">{error()}</p> : null}
      </form>
    </section>
  );
}
