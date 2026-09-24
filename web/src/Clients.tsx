import { createSignal, For, Show } from "solid-js";
import type { Client, ClientInput, Discovery, Profile } from "./api";
import Discoveries from "./Discoveries";
import { effectiveMode } from "./resolve";

type Props = {
  clients: Client[];
  profiles: Profile[];
  discoveries: Discovery[];
  onClaimDiscovery: (discovery: Discovery, name: string) => Promise<void>;
  onDismissDiscovery: (mac: string) => Promise<void>;
  onSave: (name: string, input: ClientInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
};

function splitSelectors(value: string): string[] {
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export default function Clients(props: Props) {
  const [name, setName] = createSignal("");
  const [profile, setProfile] = createSignal("");
  const [notes, setNotes] = createSignal("");
  const [addresses, setAddresses] = createSignal("");
  const [macs, setMACs] = createSignal("");
  const [prefixes, setPrefixes] = createSignal("");
  const [editing, setEditing] = createSignal<string>();
  const [manual, setManual] = createSignal(false);
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function reset() {
    setName("");
    setProfile("");
    setNotes("");
    setAddresses("");
    setMACs("");
    setPrefixes("");
    setEditing(undefined);
    setManual(false);
  }

  function edit(client: Client) {
    setEditing(client.name);
    setManual(true);
    setName(client.name);
    setProfile(client.profile);
    setNotes(client.notes);
    setAddresses(client.addresses.join("\n"));
    setMACs(client.macs.join("\n"));
    setPrefixes(client.prefixes.join("\n"));
    setError(undefined);
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name()) {
      setError("a client needs a name");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(name(), {
        profile: profile(),
        notes: notes(),
        addresses: splitSelectors(addresses()),
        macs: splitSelectors(macs()),
        prefixes: splitSelectors(prefixes()),
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

  return (
    <>
      <section class="panel">
        <div class="clients-head">
          <div>
            <h2>Add a device</h2>
            <p class="muted">
              Claim a device this server has already seen talking, or type the selectors in by hand.
            </p>
          </div>
          <div class="row-actions">
            <button
              type="button"
              data-testid="toggle-manual"
              onClick={() => {
                setManual((current) => !current);
                setEditing(undefined);
              }}
            >
              {manual() ? "Hide manual entry" : "Add manually"}
            </button>
          </div>
        </div>

        <h3 data-testid="seen-title">Seen talking to this server</h3>
        <Show
          when={props.discoveries.length > 0}
          fallback={<p class="muted" data-testid="seen-empty">Nothing new. Devices appear here when they ask for an address or send a query.</p>}
        >
          <div class="seen-devices">
            <Discoveries
              discoveries={props.discoveries}
              onClaim={props.onClaimDiscovery}
              onDismiss={props.onDismissDiscovery}
            />
          </div>
        </Show>
      </section>

      <section class="panel">
        <h2>Configured clients</h2>
        <Show when={props.clients.length === 0}>
          <p class="muted" data-testid="clients-empty">No clients yet. Claim a seen device above or add one manually.</p>
        </Show>
        <ul>
          <For each={props.clients}>
            {(client) => (
              <li data-testid="client-row">
                <div>
                  <strong>{client.name}</strong>
                  <span class="muted">
                    {client.profile} gives {effectiveMode(props.profiles, client.profile)}
                  </span>
                  <span class="selectors">
                    {[...client.addresses, ...client.prefixes].join(", ") || "no selectors"}
                  </span>
                </div>
                <div class="row-actions">
                  <button type="button" data-testid="client-edit" onClick={() => edit(client)}>
                    Edit
                  </button>
                  <button type="button" class="btn-danger" onClick={() => void remove(client.name)}>
                    Delete
                  </button>
                </div>
              </li>
            )}
          </For>
        </ul>
        {error() && !manual() ? <p class="error">{error()}</p> : null}
      </section>

      <Show when={manual()}>
        <section class="panel">
          <form onSubmit={(event) => void submit(event)}>
            <h2>{editing() ? `Edit ${editing()}` : "New client"}</h2>
            <label>
              Name
              <input
                data-testid="client-name"
                value={name()}
                disabled={editing() !== undefined}
                onInput={(event) => setName(event.currentTarget.value)}
              />
            </label>
            <label>
              Profile
              <select
                data-testid="client-profile"
                value={profile()}
                onInput={(event) => setProfile(event.currentTarget.value)}
              >
                <option value="">choose a profile</option>
                <For each={props.profiles}>{(item) => <option value={item.name}>{item.name}</option>}</For>
              </select>
            </label>
            <label>
              Notes
              <input
                data-testid="client-notes"
                value={notes()}
                onInput={(event) => setNotes(event.currentTarget.value)}
              />
            </label>
            <label>
              Addresses
              <textarea
                data-testid="client-addresses"
                value={addresses()}
                placeholder="one per line, for example 10.9.9.2"
                onInput={(event) => setAddresses(event.currentTarget.value)}
              />
            </label>
            <label>
              Hardware addresses
              <textarea
                data-testid="client-macs"
                value={macs()}
                placeholder="one per line, for example aa:bb:cc:dd:ee:01"
                onInput={(event) => setMACs(event.currentTarget.value)}
              />
            </label>
            <label>
              Prefixes
              <textarea
                data-testid="client-prefixes"
                value={prefixes()}
                placeholder="one per line, for example 10.9.8.0/24"
                onInput={(event) => setPrefixes(event.currentTarget.value)}
              />
            </label>
            <div class="row-actions">
              <button type="submit" data-testid="client-save" disabled={busy()}>
                Save
              </button>
              <button type="button" onClick={reset}>
                Cancel
              </button>
            </div>
            {error() ? <p class="error">{error()}</p> : null}
          </form>
        </section>
      </Show>
    </>
  );
}
