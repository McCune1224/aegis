import { createSignal } from "solid-js";

// SaveState is what the operator needs to see while a write travels: nothing,
// the write in flight, or the write landed. A page shows it instead of leaving
// a frozen form to explain itself.
export type SaveState = "idle" | "saving" | "saved";

// Every list page edits a row the same three ways: new, rewrite in place, or
// copy under a free key. Naming the state once is what keeps a page from
// growing an "editing" boolean and a separate "duplicating" one that drift.
export type Editor<K> =
   | { mode: "closed" }
   | { mode: "new" }
   | { mode: "edit"; key: K }
   | { mode: "duplicate"; key: K };

export function openNew<K>(): Editor<K> {
   return { mode: "new" };
}

export function openEdit<K>(key: K): Editor<K> {
   return { mode: "edit", key };
}

export function openDuplicate<K>(key: K): Editor<K> {
   return { mode: "duplicate", key };
}

export function closeEditor<K>(): Editor<K> {
   return { mode: "closed" };
}

// editorTitle is the drawer heading for a state. Closed renders nothing, so it
// borrows the new title rather than leaking an empty string to a caller. A
// string key already reads as the row's name; a numeric one falls back to the
// noun, because "Edit 7" names nothing to an operator.
export function editorTitle<K>(editor: Editor<K>, noun: string): string {
   switch (editor.mode) {
      case "new":
      case "closed":
         return `New ${noun}`;
      case "edit":
      case "duplicate": {
         const named = typeof editor.key === "string" ? editor.key : noun;
         return `${editor.mode === "edit" ? "Edit" : "Duplicate"} ${named}`;
      }
   }
}

// A named record's key is its identity, so a copy needs a free one. The first
// copy is name-copy, the next name-copy-2, and on. Taken is the live key set.
export function duplicateName(name: string, taken: Iterable<string>): string {
   const used = new Set(taken);
   let candidate = `${name}-copy`;
   for (let n = 2; used.has(candidate); n += 1) {
      candidate = `${name}-copy-${n}`;
   }
   return candidate;
}

// createCrud is the reactive half: which row the drawer is for, whether a
// request is in flight, and the message a failed one left. run wraps the save
// so a page cannot forget to clear busy or to surface the error.
export function createCrud<K>() {
   const [editor, setEditor] = createSignal<Editor<K>>({ mode: "closed" });
   const [busy, setBusy] = createSignal(false);
   const [error, setError] = createSignal<string>();
   const [saveState, setSaveState] = createSignal<SaveState>("idle");

   return {
      editor,
      busy,
      error,
      saveState,
      setError,

      clearSaved(): void {
         setSaveState("idle");
      },

      openNew(): void {
         setError(undefined);
         setEditor({ mode: "new" });
      },

      openEdit(key: K): void {
         setError(undefined);
         setEditor({ mode: "edit", key });
      },

      openDuplicate(key: K): void {
         setError(undefined);
         setEditor({ mode: "duplicate", key });
      },

      close(): void {
         setEditor({ mode: "closed" });
      },

      async run(action: () => Promise<void>): Promise<void> {
         setSaveState("saving");
         setBusy(true);
         setError(undefined);
         let failed = false;
         try {
            await action();
         } catch (cause) {
            failed = true;
            setError(String(cause));
         } finally {
            setBusy(false);
            setSaveState(failed ? "idle" : "saved");
         }
      },
   };
}
