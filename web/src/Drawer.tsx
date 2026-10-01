import { createEffect, Show } from "solid-js";
import type { JSX } from "@solidjs/web";

type Props = {
   open: boolean;
   title: string;
   onClose: () => void;
   children: JSX.Element;
};

// The one editing surface. Everything that used to be a form under a list
// opens here instead. Escape and the scrim both close it.
export default function Drawer(props: Props) {
   let panel: HTMLDivElement | undefined;

   // The effect returns its cleanup, which is what Solid 2 runs on the next
   // change and on unmount. onCleanup inside the effect body has no owner and
   // never runs, so the listener would outlive the drawer.
   createEffect(
      () => props.open,
      (open) => {
         if (!open) {
            return;
         }
         const onKey = (event: KeyboardEvent) => {
            if (event.key === "Escape") {
               props.onClose();
            }
         };
         window.addEventListener("keydown", onKey);
         return () => window.removeEventListener("keydown", onKey);
      },
   );

   createEffect(
      () => props.open,
      (open) => {
         if (open) {
            setTimeout(() => panel?.focus(), 0);
         }
      },
   );

   return (
      <Show when={props.open}>
         <div class="scrim" onClick={() => props.onClose()} />
         <div
            class="drawer"
            role="dialog"
            aria-modal="true"
            aria-label={props.title}
            tabindex="-1"
            ref={(element) => {
               panel = element as HTMLDivElement;
            }}
         >
            <header class="drawer-head">
               <h2>{props.title}</h2>
               <button type="button" class="icon-btn" aria-label="close" onClick={() => props.onClose()}>
                  ×
               </button>
            </header>
            <div class="drawer-body">{props.children}</div>
         </div>
      </Show>
   );
}
