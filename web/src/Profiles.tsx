import { createSignal, For, Show } from "solid-js";
import type { Profile, ProfileInput, SafesearchEngine } from "./api";
import { BLOCKING_MODES } from "./api";
import CrudActions from "./CrudActions";
import { createCrud, duplicateName, editorTitle } from "./crud";
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
   const crud = createCrud<string>();
   const [name, setName] = createSignal("");
   const [parent, setParent] = createSignal("");
   const [mode, setMode] = createSignal("");
   const [custom, setCustom] = createSignal("");
   const [rowError, setRowError] = createSignal<string>();

   function clear() {
      setName("");
      setParent("");
      setMode("");
      setCustom("");
   }

   function fill(profile: Profile) {
      setName(profile.name);
      setParent(profile.extends ?? "");
      setMode(profile.mode ?? "");
      setCustom(profile.custom ?? "");
   }

   function openNew() {
      clear();
      crud.openNew();
   }

   function edit(profile: Profile) {
      fill(profile);
      crud.openEdit(profile.name);
   }

   // The name is the key the profiles API writes through, so the copy asks for
   // a free one and keeps everything else.
   function duplicate(profile: Profile) {
      fill(profile);
      setName(duplicateName(profile.name, props.profiles.map((entry) => entry.name)));
      crud.openDuplicate(profile.name);
   }

   // editingKey is the row whose stored record the drawer is showing. Duplicate
   // has not been saved yet, so no stored record exists to write to.
   function editingKey(): string | undefined {
      const editor = crud.editor();
      return editor.mode === "edit" ? editor.key : undefined;
   }

   async function submit(event: SubmitEvent) {
      event.preventDefault();
      if (!name()) {
         crud.setError("a profile needs a name");
         return;
      }
      const input: ProfileInput = { extends: parent(), mode: mode(), custom: custom() };
      await crud.run(async () => {
         await props.onSave(name(), input);
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

   async function makeDefault(target: string) {
      setRowError(undefined);
      try {
         await props.onSetDefault(target);
      } catch (cause) {
         setRowError(String(cause));
      }
   }

   function enabledEngines(): string[] {
      const profile = editingKey();
      if (!profile) {
         return [];
      }
      return props.safesearch.filter((engine) => engine.profiles.includes(profile)).map((engine) => engine.id);
   }

   async function toggleEngine(id: string) {
      const profile = editingKey();
      if (!profile) {
         return;
      }
      await crud.run(async () => {
         await props.onSaveSafesearch(profile, toggled(enabledEngines(), id));
      });
   }

   return (
      <>
         <div class="workbench">
            <div class="workbench-list">
               <div class="subbar">
                  <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
                     Profiles
                  </h2>
                  <button type="button" class="btn-mini" onClick={openNew}>
                     + New profile
                  </button>
               </div>
               <Show when={rowError()}>
                  <p class="alert-line error-line" role="alert">
                     {rowError()}
                  </p>
               </Show>
               <For each={props.profiles}>
                  {(profile) => (
                     <div
                        role="button"
                        tabindex="0"
                        class={editingKey() === profile.name ? "entry selected" : "entry"}
                        data-testid="profile-row"
                        onClick={() => edit(profile)}
                        onKeyDown={(event) => {
                           if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              edit(profile);
                           }
                        }}
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
                        <CrudActions
                           testid="profile"
                           busy={crud.busy()}
                           onEdit={() => edit(profile)}
                           onDuplicate={() => duplicate(profile)}
                           onDelete={() => void remove(profile.name)}
                        />
                     </div>
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

         <Drawer
            open={crud.editor().mode !== "closed"}
            title={editorTitle(crud.editor(), "profile")}
            onClose={crud.close}
         >
            <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="profile-name"
                     value={name()}
                     disabled={editingKey() !== undefined}
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
                           <Show when={profile.name !== editingKey()}>
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
               {crud.error() ? <p class="error">{crud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <Show when={editingKey() && editingKey() !== props.defaultProfile}>
                     <button type="button" class="btn-ghost" onClick={() => void makeDefault(editingKey()!)}>
                        Make default
                     </button>
                  </Show>
                  <button type="submit" class="btn" data-testid="profile-save" disabled={crud.busy()}>
                     Save
                  </button>
               </div>
               <Show when={editingKey()}>
                  <fieldset class="services" data-testid="profile-safesearch">
                     <legend>Safe search</legend>
                     <For each={props.safesearch}>
                        {(engine) => (
                           <label class="toggle">
                              <input
                                 type="checkbox"
                                 data-testid={`safesearch-${engine.id}`}
                                 checked={enabledEngines().includes(engine.id)}
                                 disabled={crud.busy()}
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
