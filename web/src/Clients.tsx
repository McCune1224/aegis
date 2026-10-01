import { createSignal, For, Show } from "solid-js";
import type { Client, ClientInput, Discovery, Observed, Profile } from "./api";
import CrudActions from "./CrudActions";
import Discoveries from "./Discoveries";
import { createCrud, duplicateName, editorTitle } from "./crud";
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
   const crud = createCrud<string>();
   const [name, setName] = createSignal("");
   const [profile, setProfile] = createSignal("");
   const [notes, setNotes] = createSignal("");
   const [addresses, setAddresses] = createSignal("");
   const [macs, setMACs] = createSignal("");
   const [prefixes, setPrefixes] = createSignal("");
   const [rowError, setRowError] = createSignal<string>();

   function clear() {
      setName("");
      setProfile("");
      setNotes("");
      setAddresses("");
      setMACs("");
      setPrefixes("");
   }

   function fill(client: Client) {
      setName(client.name);
      setProfile(client.profile);
      setNotes(client.notes);
      setAddresses(client.addresses.join("\n"));
      setMACs(client.macs.join("\n"));
      setPrefixes(client.prefixes.join("\n"));
   }

   function openNew() {
      clear();
      crud.openNew();
   }

   function edit(client: Client) {
      fill(client);
      crud.openEdit(client.name);
   }

   // A copy keeps the profile, the notes, and every selector, and asks for a
   // name, because the name is the key the clients API writes through.
   function duplicate(client: Client) {
      fill(client);
      setName(duplicateName(client.name, props.clients.map((entry) => entry.name)));
      crud.openDuplicate(client.name);
   }

   // editingKey is the stored record the drawer is writing to. Duplicate has
   // not been saved, so there is no key to write through yet.
   function editingKey(): string | undefined {
      const editor = crud.editor();
      return editor.mode === "edit" ? editor.key : undefined;
   }

   async function submit(event: SubmitEvent) {
      event.preventDefault();
      if (!name()) {
         crud.setError("a client needs a name");
         return;
      }
      const input: ClientInput = {
         profile: profile(),
         notes: notes(),
         addresses: splitSelectors(addresses()),
         macs: splitSelectors(macs()),
         prefixes: splitSelectors(prefixes()),
      };
      const key = name();
      await crud.run(async () => {
         await props.onSave(key, input);
         clear();
         crud.close();
      });
   }

   async function remove(target: string) {
      setRowError(undefined);
      try {
         await props.onDelete(target);
         const editor = crud.editor();
         if (editor.mode === "edit" && editor.key === target) {
            crud.close();
         }
      } catch (cause) {
         setRowError(String(cause));
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
                  <button type="button" class="btn-mini" data-testid="toggle-manual" onClick={openNew}>
                     + New client
                  </button>
               </div>
               <Show when={rowError()}>
                  <p class="alert-line error-line" role="alert">
                     {rowError()}
                  </p>
               </Show>
               <Show when={props.clients.length === 0}>
                  <p class="empty" data-testid="clients-empty">No clients yet. Claim a seen device or add one manually.</p>
               </Show>
               <For each={props.clients}>
                  {(client) => (
                     <div
                        role="button"
                        tabindex="0"
                        class={editingKey() === client.name ? "entry selected" : "entry"}
                        data-testid="client-row"
                        onClick={() => edit(client)}
                        onKeyDown={(event) => {
                           if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              edit(client);
                           }
                        }}
                     >
                        <span class="entry-title" data-testid="client-title">
                           {client.name}
                           <Show when={client.profile}>
                              <span class="badge profile">{client.profile}</span>
                           </Show>
                        </span>
                        <span class="entry-sub">
                           {[...client.addresses, ...client.prefixes].join(", ") || "no selectors"}
                        </span>
                        <CrudActions
                           testid="client"
                           busy={crud.busy()}
                           onEdit={() => edit(client)}
                           onDuplicate={() => duplicate(client)}
                           onDelete={() => void remove(client.name)}
                        />
                     </div>
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

         <Drawer
            open={crud.editor().mode !== "closed"}
            title={editorTitle(crud.editor(), "client")}
            onClose={crud.close}
         >
            <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="client-name"
                     value={name()}
                     disabled={editingKey() !== undefined}
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
               {crud.error() ? <p class="error">{crud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <button type="submit" class="btn" data-testid="client-save" disabled={crud.busy()}>
                     Save
                  </button>
               </div>
            </form>
         </Drawer>
      </>
   );
}
