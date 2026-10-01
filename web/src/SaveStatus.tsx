import type { JSX } from "@solidjs/web";
import { createEffect, createSignal, Show } from "solid-js";
import type { SaveState } from "./crud";

type Props = {
   state: SaveState;
};

// The one status line for a write. Saving stays while the request travels so a
// slow save is explained rather than guessed at, and saved clears itself so the
// badge never becomes decoration.
export default function SaveStatus(props: Props): JSX.Element {
   const [showSaved, setShowSaved] = createSignal(false);

   createEffect(
      () => props.state,
      (state) => {
         const saved = state === "saved";
         setShowSaved(saved);
         if (!saved) {
            return;
         }
         const timer = setTimeout(() => setShowSaved(false), 2500);
         return () => clearTimeout(timer);
      },
   );

   return (
      <Show when={props.state === "saving" || showSaved()}>
         <span
            class={props.state === "saving" ? "badge kind" : "badge allow"}
            role="status"
            aria-busy={props.state === "saving" ? "true" : "false"}
            data-testid="save-status"
         >
            {props.state === "saving" ? "saving…" : "saved"}
         </span>
      </Show>
   );
}
