import { createSignal, For, Show } from "solid-js";
import type { AccessSettings, Profile } from "./api";

type Props = {
  profiles: Profile[];
  defaultProfile: string;
  onSetDefault: (name: string) => Promise<void>;
  access: AccessSettings;
  onSaveAccess: (input: AccessSettings) => Promise<void>;
  windowMinutes: number;
  onSetWindow: (minutes: number) => void;
  live: boolean;
  onSetLive: (live: boolean) => void;
};

// Settings covers what the API can change today plus the dashboard's display
// preferences, which live in localStorage. Server-side knobs (upstream,
// retention) land here when the API grows them.
export default function Settings(props: Props) {
  const [saved, setSaved] = createSignal(false);
  const [allowEntry, setAllowEntry] = createSignal("");
  const [denyEntry, setDenyEntry] = createSignal("");
  const [accessError, setAccessError] = createSignal<string>();
  const [accessBusy, setAccessBusy] = createSignal(false);

  function persistWindow(minutes: number) {
    props.onSetWindow(minutes);
    localStorage.setItem("aegis.window", String(minutes));
  }

  function persistLive(live: boolean) {
    props.onSetLive(live);
    localStorage.setItem("aegis.live", live ? "1" : "0");
    setSaved(true);
    setTimeout(() => setSaved(false), 1500);
  }

  async function saveAccess(next: AccessSettings): Promise<boolean> {
    setAccessBusy(true);
    setAccessError(undefined);
    try {
      await props.onSaveAccess(next);
      return true;
    } catch (cause) {
      setAccessError(String(cause));
      return false;
    } finally {
      setAccessBusy(false);
    }
  }

  function withEntry(kind: "allowed" | "disallowed", entry: string): AccessSettings {
    const next: AccessSettings = { allowed: [...props.access.allowed], disallowed: [...props.access.disallowed] };
    next[kind].push(entry);
    return next;
  }

  function withoutEntry(kind: "allowed" | "disallowed", entry: string): AccessSettings {
    const next: AccessSettings = { allowed: [...props.access.allowed], disallowed: [...props.access.disallowed] };
    next[kind] = next[kind].filter((row) => row !== entry);
    return next;
  }

  async function submitAllow(event: SubmitEvent) {
    event.preventDefault();
    const entry = allowEntry().trim();
    if (!entry) return;
    if (await saveAccess(withEntry("allowed", entry))) {
      setAllowEntry("");
    }
  }

  async function submitDeny(event: SubmitEvent) {
    event.preventDefault();
    const entry = denyEntry().trim();
    if (!entry) return;
    if (await saveAccess(withEntry("disallowed", entry))) {
      setDenyEntry("");
    }
  }

  async function removeEntry(kind: "allowed" | "disallowed", entry: string) {
    await saveAccess(withoutEntry(kind, entry));
  }

  return (
    <div class="screen-inner">
      <section class="panel">
        <header>
          <h2>Default policy</h2>
        </header>
        <form>
          <label>
            Profile for unidentified clients
            <select
              data-testid="settings-default-profile"
              value={props.defaultProfile}
              onInput={(event) => void props.onSetDefault(event.currentTarget.value)}
            >
              <For each={props.profiles}>{(item) => <option value={item.name}>{item.name}</option>}</For>
            </select>
          </label>
          <p class="muted">Devices without a client entry answer with this profile. Existing clients keep their own.</p>
        </form>
      </section>

      <section class="panel">
        <header>
          <h2>Client access</h2>
        </header>
        <p class="muted">Disallowed clients are refused before any processing. Once the allowed list has an entry, only the clients it lists are served.</p>
        <form class="access-form" onSubmit={(event) => void submitAllow(event)}>
          <h2>Allowed</h2>
          <ul class="tag-list">
            <For each={props.access.allowed}>
              {(entry) => (
                <li class="tag" data-testid="access-allow-tag">
                  {entry}
                  <button
                    type="button"
                    class="tag-remove"
                    data-testid="access-allow-remove"
                    aria-label={`remove ${entry} from the allowed list`}
                    onClick={() => void removeEntry("allowed", entry)}
                  >
                    ×
                  </button>
                </li>
              )}
            </For>
            <Show when={props.access.allowed.length === 0}>
              <li class="muted">everyone is served</li>
            </Show>
          </ul>
          <label>
            Allow a client
            <input
              data-testid="access-allow-input"
              value={allowEntry()}
              placeholder="10.0.0.0/24"
              onInput={(event) => setAllowEntry(event.currentTarget.value)}
            />
          </label>
          <div class="row-actions">
            <button type="submit" data-testid="access-allow-add" disabled={accessBusy()}>
              Add
            </button>
          </div>
        </form>
        <form class="access-form" onSubmit={(event) => void submitDeny(event)}>
          <h2>Disallowed</h2>
          <ul class="tag-list">
            <For each={props.access.disallowed}>
              {(entry) => (
                <li class="tag" data-testid="access-deny-tag">
                  {entry}
                  <button
                    type="button"
                    class="tag-remove"
                    data-testid="access-deny-remove"
                    aria-label={`remove ${entry} from the disallowed list`}
                    onClick={() => void removeEntry("disallowed", entry)}
                  >
                    ×
                  </button>
                </li>
              )}
            </For>
            <Show when={props.access.disallowed.length === 0}>
              <li class="muted">nobody is refused</li>
            </Show>
          </ul>
          <label>
            Disallow a client
            <input
              data-testid="access-deny-input"
              value={denyEntry()}
              placeholder="192.168.1.50/32"
              onInput={(event) => setDenyEntry(event.currentTarget.value)}
            />
          </label>
          <div class="row-actions">
            <button type="submit" data-testid="access-deny-add" disabled={accessBusy()}>
              Add
            </button>
          </div>
        </form>
        {accessError() ? <p class="error">{accessError()}</p> : null}
      </section>

      <section class="panel">
        <header>
          <h2>Display</h2>
          <Show when={saved()}>
            <span class="badge allow">saved</span>
          </Show>
        </header>
        <form>
          <label>
            Dashboard window
            <select
              data-testid="settings-window"
              value={props.windowMinutes}
              onInput={(event) => persistWindow(Number(event.currentTarget.value))}
            >
              <option value={60}>last hour</option>
              <option value={1440}>last 24 hours</option>
            </select>
          </label>
          <label class="toggle">
            <input
              type="checkbox"
              data-testid="settings-live"
              checked={props.live}
              onInput={(event) => persistLive(event.currentTarget.checked)}
            />
            <span>start the query log in live mode</span>
          </label>
        </form>
      </section>
    </div>
  );
}
