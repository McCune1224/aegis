import { createSignal, For, Show } from "solid-js";
import type { Client, ClientInput, Discovery, Observed, Profile } from "./api";
import Discoveries from "./Discoveries";
import Drawer from "./Drawer";
import { effectiveMode } from "./resolve";

type Props = {
  clients: Client[];
  profiles: Profile[];
  discoveries: Discovery[];
  observed: Observed[];
  onClaimObserved: (entry: Observed) => Promise<void>;
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
  const [drawer, setDrawer] = createSignal(false);
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
  }

  function openNew() {
    reset();
    setDrawer(true);
    setError(undefined);
  }

  function edit(client: Client) {
    setEditing(client.name);
    setName(client.name);
    setProfile(client.profile);
    setNotes(client.notes);
    setAddresses(client.addresses.join("\n"));
    setMACs(client.macs.join("\n"));
    setPrefixes(client.prefixes.join("\n"));
    setError(undefined);
    setDrawer(true);
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

  return (
    <>
      <div class="workbench">
        <div class="workbench-list">
          <div class="subbar">
            <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
              Clients
            </h2>
            <button type="button" class="btn-mini" data-testid="toggle-manual" onClick={() => openNew()}>
              + New client
            </button>
          </div>
          <Show when={props.clients.length === 0}>
            <p class="empty" data-testid="clients-empty">No clients yet. Claim a seen device or add one manually.</p>
          </Show>
          <For each={props.clients}>
            {(client) => (
              <button
                type="button"
                class={editing() === client.name ? "entry selected" : "entry"}
                data-testid="client-row"
                onClick={() => edit(client)}
              >
                <span class="entry-title" data-testid="client-edit">
                  {client.name}
                  <Show when={client.profile}>
                    <span class="badge profile">{client.profile}</span>
                  </Show>
                </span>
                <span class="entry-sub">
                  {[...client.addresses, ...client.prefixes].join(", ") || "no selectors"}
                </span>
              </button>
            )}
          </For>
        </div>

        <div class="workbench-main">
          <section data-testid="seen-title" style={{ "margin-bottom": "18px" }}>
            <h2 style={{ "font-size": "10.5px", "font-weight": "700", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "margin-bottom": "10px" }}>
              Seen talking to this server
            </h2>
            <Show
              when={props.observed.length > 0 || props.discoveries.length > 0}
              fallback={
                <p class="muted" data-testid="seen-empty">
                  Nothing new. Devices appear here when they ask for an address or send a query.
                </p>
              }
            >
              <div style={{ display: "flex", "flex-direction": "column", gap: "10px", "max-width": "620px" }}>
                <Show when={props.observed.length > 0}>
                  <div style={{ display: "flex", "flex-direction": "column", gap: "6px" }} data-testid="observed-list">
                    <For each={props.observed}>
                      {(entry) => (
                        <div style={{ display: "flex", "align-items": "center", gap: "12px", padding: "8px 10px", "border-radius": "10px", background: "var(--panel-2, rgba(20,20,20,0.04))" }} data-testid="observed-row">
                          <span style={{ "font-weight": 600, "min-width": "130px" }}>{entry.client}</span>
                          <span class="muted" style={{ "font-size": "11.5px" }}>
                            {entry.queries} {entry.queries === 1 ? "query" : "queries"} · last seen{" "}
                            {new Date(entry.last_seen).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}
                          </span>
                          <span style={{ "margin-left": "auto" }}>
                            <Show
                              when={!entry.claimed}
                              fallback={
                                <span class="muted" style={{ "font-size": "11px" }}>
                                  claimed
                                </span>
                              }
                            >
                              <button
                                type="button"
                                class="btn-mini"
                                data-testid={`claim-${entry.client}`}
                                onClick={() => void props.onClaimObserved(entry)}
                              >
                                Claim
                              </button>
                            </Show>
                          </span>
                        </div>
                      )}
                    </For>
                  </div>
                </Show>
                <Show when={props.discoveries.length > 0}>
                  <Discoveries
                    discoveries={props.discoveries}
                    onClaim={props.onClaimDiscovery}
                    onDismiss={props.onDismissDiscovery}
                  />
                </Show>
              </div>
            </Show>
          </section>

          <div class="inspector-section" style={{ "max-width": "620px" }}>
            <h3>How clients work</h3>
            <p class="muted">
              A client is one device or a group of devices, matched by address, hardware address, or prefix. Its
              profile decides how blocked names are answered. Select a client on the left to edit it, or claim a
              device the server has seen.
            </p>
          </div>
        </div>
      </div>

      <Drawer open={drawer()} title={editing() ? `Edit ${editing()}` : "New client"} onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
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
          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <Show when={editing()}>
              <button type="button" class="btn-danger" onClick={() => void remove(editing()!)}>
                Delete
              </button>
            </Show>
            <button type="submit" class="btn" data-testid="client-save" disabled={busy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
