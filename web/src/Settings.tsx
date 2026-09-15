import { createSignal, For, Show } from "solid-js";
import type { Profile } from "./api";

type Props = {
  profiles: Profile[];
  defaultProfile: string;
  onSetDefault: (name: string) => Promise<void>;
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
