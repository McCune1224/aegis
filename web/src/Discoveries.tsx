import { createSignal, For } from "solid-js";
import type { Discovery } from "./api";

type Props = {
  discoveries: Discovery[];
  onClaim: (discovery: Discovery, name: string) => Promise<void>;
  onDismiss: (mac: string) => Promise<void>;
};

// suggests a client name from what the device told the DHCP server, falling
// back to its hardware address so the field is never empty.
export function suggestedName(discovery: Discovery): string {
  const hostname = (discovery.hostname ?? "").trim();
  if (hostname) {
    return hostname;
  }
  return discovery.mac.replace(/:/g, "");
}

export default function Discoveries(props: Props) {
  const [names, setNames] = createSignal<Record<string, string>>({});
  const [busy, setBusy] = createSignal<string>();
  const [error, setError] = createSignal<string>();

  function nameFor(discovery: Discovery): string {
    return names()[discovery.mac] ?? suggestedName(discovery);
  }

  async function claim(discovery: Discovery) {
    setBusy(discovery.mac);
    setError(undefined);
    try {
      await props.onClaim(discovery, nameFor(discovery));
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(undefined);
    }
  }

  async function dismiss(discovery: Discovery) {
    setBusy(discovery.mac);
    setError(undefined);
    try {
      await props.onDismiss(discovery.mac);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(undefined);
    }
  }

  return (
    <For each={props.discoveries}>
      {(discovery) => (
        <section class="panel discovery" data-testid="discovery-card">
          <header>
            <h2>New device asked for an address</h2>
          </header>
          <p class="muted">
            {discovery.address}
            {discovery.hostname ? ` · ${discovery.hostname}` : ""} · {discovery.mac}
          </p>
          <div class="row-actions">
            <label>
              Name
              <input
                data-testid="discovery-name"
                value={nameFor(discovery)}
                onInput={(event) =>
                  setNames({ ...names(), [discovery.mac]: event.currentTarget.value })
                }
              />
            </label>
            <button
              type="button"
              data-testid="discovery-add"
              disabled={busy() === discovery.mac}
              onClick={() => void claim(discovery)}
            >
              Add as a client
            </button>
            <button
              type="button"
              data-testid="discovery-dismiss"
              disabled={busy() === discovery.mac}
              onClick={() => void dismiss(discovery)}
            >
              Dismiss
            </button>
          </div>
        </section>
      )}
    </For>
  );
}
