import { createSignal, For, Show } from "solid-js";
import type { Rewrite } from "./api";
import CrudActions from "./CrudActions";
import SaveStatus from "./SaveStatus";
import DataTable, { type Column } from "./DataTable";
import { createCrud, editorTitle } from "./crud";
import Drawer from "./Drawer";

type Props = {
   rewrites: Rewrite[];
   onSave: (pattern: string, target: string) => Promise<void>;
   onDelete: (pattern: string) => Promise<void>;
};

export default function Rewrites(props: Props) {
   const crud = createCrud<string>();
   const [pattern, setPattern] = createSignal("");
   const [target, setTarget] = createSignal("");
   const [rowError, setRowError] = createSignal<string>();

   function clear() {
      setPattern("");
      setTarget("");
   }

   function fill(rewrite: Rewrite) {
      setPattern(rewrite.pattern);
      setTarget(rewrite.target);
   }

   function openNew() {
      clear();
      crud.openNew();
   }

   function edit(rewrite: Rewrite) {
      fill(rewrite);
      crud.openEdit(rewrite.pattern);
   }

   // The key of a rewrite is a name pattern, so a copy cannot take a -copy
   // suffix: that is not a name any resolver would match. The target carries
   // over and the pattern comes up empty for the operator to type.
   function duplicate(rewrite: Rewrite) {
      setPattern("");
      setTarget(rewrite.target);
      crud.openDuplicate(rewrite.pattern);
   }

   async function submit(event: SubmitEvent) {
      event.preventDefault();
      if (!pattern().trim()) {
         crud.setError("a rewrite needs a pattern");
         return;
      }
      if (!target().trim()) {
         crud.setError("a rewrite needs a target");
         return;
      }
      const key = pattern().trim();
      const value = target().trim();
      await crud.run(async () => {
         await props.onSave(key, value);
         clear();
         crud.close();
      });
   }

   async function remove(target: string) {
      setRowError(undefined);
      try {
         await props.onDelete(target);
      } catch (cause) {
         setRowError(String(cause));
      }
   }

   const columns: Column<Rewrite>[] = [
      {
         key: "pattern",
         label: "Name",
         sortable: true,
         value: (rewrite) => rewrite.pattern,
         render: (rewrite) => <span class="mono">{rewrite.pattern}</span>,
      },
      {
         key: "target",
         label: "Answers with",
         sortable: true,
         value: (rewrite) => rewrite.target,
         render: (rewrite) => <span class="mono muted">{rewrite.target}</span>,
      },
      {
         key: "actions",
         label: "",
         value: () => "",
         render: (rewrite) => (
            <CrudActions
               testid="rewrite"
               label={rewrite.pattern}
               busy={crud.busy()}
               onEdit={() => edit(rewrite)}
               onDuplicate={() => duplicate(rewrite)}
               onDelete={() => void remove(rewrite.pattern)}
            />
         ),
      },
   ];

   return (
      <>
         <div class="view">
            <div class="subbar">
               <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
                  {props.rewrites.length} rewrites
               </h2>
               <button type="button" class="btn" data-testid="rewrites-new" onClick={openNew}>
                  + New rewrite
               </button>
            </div>
            <Show when={rowError()}>
               <p class="alert-line error-line" role="alert">
                  {rowError()}
               </p>
            </Show>
            <div class="view-scroll">
               <div class="view-inner">
                  <div class="sheet">
                     <DataTable
                        columns={columns}
                        rows={props.rewrites}
                        rowKey={(rewrite) => rewrite.pattern}
                        testid="rewrite-rows"
                        rowTestid={() => "rewrite-row"}
                        empty="no rewrites yet"
                     />
                  </div>
                  <p class="muted" style={{ "max-width": "620px", "font-size": "12px" }}>
                     A name rewrite answers from here no matter what the rules say. A rewrite to another name returns that
                     name, and the rules still apply to it.
                  </p>
               </div>
            </div>
         </div>

         <Drawer
            open={crud.editor().mode !== "closed"}
            title={editorTitle(crud.editor(), "rewrite")}
            onClose={crud.close}
         >
            <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
               <label>
                  Name
                  <input
                     data-testid="rewrite-pattern"
                     value={pattern()}
                     placeholder="nas.local or *.nas.local"
                     disabled={crud.editor().mode === "edit"}
                     onInput={(event) => setPattern(event.currentTarget.value)}
                  />
               </label>
               <label>
                  Target
                  <input
                     data-testid="rewrite-target"
                     value={target()}
                     placeholder="192.168.1.50 or another name"
                     onInput={(event) => setTarget(event.currentTarget.value)}
                  />
               </label>
               {crud.error() ? <p class="error">{crud.error()}</p> : null}
               <div class="row-actions" style={{ "justify-content": "flex-end" }}>
                  <SaveStatus state={crud.saveState()} />
                  <button type="submit" class="btn" data-testid="rewrite-save" disabled={crud.busy()}>
                     Save
                  </button>
               </div>
            </form>
         </Drawer>
      </>
   );
}
