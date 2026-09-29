import { createSignal, For, Show } from "solid-js";
import type { Profile, ProfileInput, SafesearchEngine } from "./api";
import { BLOCKING_MODES } from "./api";
import { toggled } from "./services";
import Drawer from "./Drawer";

type Props = {
  profiles: Profile[];
  defaultProfile: string;
  safesearch: SafesearchEngine[];
  onSave: (name: string, input: ProfileInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
  onSetDefault: (name: string) => Promise<void>;
  onSaveSafesearch: (name: string, engines: string[]) => Promise<void>;
};

export default function Profiles(props: Props) {
  const [name, setName] = createSignal("");
  const [parent, setParent] = createSignal("");
  const [mode, setMode] = createSignal("");
  const [custom, setCustom] = createSignal("");
  const [editing, setEditing] = createSignal<string>();
  const [drawer, setDrawer] = createSignal(false);
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setName("");
    setParent("");
    setMode("");
    setCustom("");
    setEditing(undefined);
  }

  function openNew() {
    reset();
    setDrawer(true);
    setError(undefined);
  }

  function edit(profile: Profile) {
    setEditing(profile.name);
    setName(profile.name);
    setParent(profile.extends ?? "");
    setMode(profile.mode ?? "");
    setCustom(profile.custom ?? "");
    setError(undefined);
    setDrawer(true);
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
      setDrawer(false);
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
        setDrawer(false);
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

  function enabledEngines(): string[] {
    const profile = editing();
    if (!profile) {
      return [];
    }
    return props.safesearch.filter((engine) => engine.profiles.includes(profile)).map((engine) => engine.id);
  }

  async function toggleEngine(id: string) {
    const profile = editing();
    if (!profile) {
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSaveSafesearch(profile, toggled(enabledEngines(), id));
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <div class="workbench">
        <div class="workbench-list">
          <div class="subbar">
            <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
              Profiles
            </h2>
            <button type="button" class="btn-mini" onClick={() => openNew()}>
              + New profile
            </button>
          </div>
          <For each={props.profiles}>
            {(profile) => (
              <button
                type="button"
                class={editing() === profile.name ? "entry selected" : "entry"}
                data-testid="profile-row"
                onClick={() => edit(profile)}
              >
                <span class="entry-title">
                  {profile.name}
                  <Show when={profile.name === props.defaultProfile}>
                    <span class="badge allow" data-testid="profile-default">
                      default
                    </span>
                  </Show>
                </span>
                <span class="entry-sub">
                  {profile.mode ?? "inherited"}
                  {profile.extends ? ` from ${profile.extends}` : ""}
                  {profile.custom ? ` ${profile.custom}` : ""}
                </span>
              </button>
            )}
          </For>
        </div>

        <div class="workbench-main">
          <div class="inspector-section" style={{ "max-width": "620px" }}>
            <h3>How profiles work</h3>
            <p class="muted">
              A profile is one blocking policy: the mode a blocked name is answered with, optionally inherited from a
              parent. Every unidentified client gets the default profile. Select a profile to edit it, change the
              default, or create a new one.
            </p>
          </div>
        </div>
      </div>

      <Drawer open={drawer()} title={editing() ? `Edit ${editing()}` : "New profile"} onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
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
              <For each={BLOCKING_MODES}>{(value) => <option value={value}>{value}</option>}</For>
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
          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <Show when={editing() && editing() !== props.defaultProfile}>
              <button type="button" class="btn-ghost" onClick={() => void makeDefault(editing()!)}>
                Make default
              </button>
            </Show>
            <Show when={editing()}>
              <button type="button" class="btn-danger" onClick={() => void remove(editing()!)}>
                Delete
              </button>
            </Show>
            <button type="submit" class="btn" data-testid="profile-save" disabled={busy()}>
              Save
            </button>
          </div>
          <Show when={editing()}>
            <fieldset class="services" data-testid="profile-safesearch">
              <legend>Safe search</legend>
              <For each={props.safesearch}>
                {(engine) => (
                  <label class="toggle">
                    <input
                      type="checkbox"
                      data-testid={`safesearch-${engine.id}`}
                      checked={enabledEngines().includes(engine.id)}
                      disabled={busy()}
                      onChange={() => void toggleEngine(engine.id)}
                    />
                    {engine.name}
                  </label>
                )}
              </For>
            </fieldset>
          </Show>
        </form>
      </Drawer>
    </>
  );
}
