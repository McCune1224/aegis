import type { JSX } from "@solidjs/web";
import { createEffect, createSignal } from "solid-js";

type Props = {
   testid: string;
   busy?: boolean;
   // label names the row for assistive technology. A column of identical
   // Delete buttons reads as Delete, Delete, Delete otherwise, and the phrase
   // starts with the visible word so the spoken name still matches the control.
   label?: string;
   onEdit: () => void;
   onDuplicate: () => void;
   onDelete: () => void;
};

const ARM_SECONDS = 4;

// The one row-action row: edit, duplicate, delete, in that order on every list,
// under one testid prefix. A page adds its own buttons beside it (refresh,
// preview) but never a second copy of these three.
//
// Delete arms first and fires on the second press. A one-click delete next to
// duplicate is a misclick waiting to happen, and the alternative dialog on
// every row is heavier than the mistake it prevents.
export default function CrudActions(props: Props): JSX.Element {
   const [armed, setArmed] = createSignal(false);

   createEffect(
      () => armed(),
      (isArmed) => {
         if (!isArmed) {
            return;
         }
         const timer = setTimeout(() => setArmed(false), ARM_SECONDS * 1000);
         return () => clearTimeout(timer);
      },
   );

   const named = (verb: string) => (props.label ? `${verb} ${props.label}` : undefined);

   return (
      <div class="row-actions">
         <button
            type="button"
            class="btn-mini"
            data-testid={`${props.testid}-edit`}
            disabled={props.busy}
            aria-label={named("Edit")}
            onClick={(event) => {
               event.stopPropagation();
               setArmed(false);
               props.onEdit();
            }}
         >
            Edit
         </button>
         <button
            type="button"
            class="btn-mini"
            data-testid={`${props.testid}-duplicate`}
            disabled={props.busy}
            aria-label={named("Duplicate")}
            onClick={(event) => {
               event.stopPropagation();
               setArmed(false);
               props.onDuplicate();
            }}
         >
            Duplicate
         </button>
         <button
            type="button"
            class={armed() ? "btn-mini btn-danger" : "btn-mini"}
            data-testid={`${props.testid}-delete`}
            data-armed={armed() ? "true" : undefined}
            disabled={props.busy}
            aria-label={named(armed() ? "Confirm delete" : "Delete")}
            onClick={(event) => {
               event.stopPropagation();
               if (!armed()) {
                  setArmed(true);
                  return;
               }
               setArmed(false);
               props.onDelete();
            }}
         >
            {armed() ? "Confirm delete" : "Delete"}
         </button>
      </div>
   );
}
