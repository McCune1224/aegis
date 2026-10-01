import type { JSX } from "@solidjs/web";

type Props = {
   testid: string;
   busy?: boolean;
   onEdit: () => void;
   onDuplicate: () => void;
   onDelete: () => void;
};

// The one row-action row: edit, duplicate, delete, in that order on every list,
// under one testid prefix. A page adds its own buttons beside it (refresh,
// preview) but never a second copy of these three.
export default function CrudActions(props: Props): JSX.Element {
   return (
      <div class="row-actions">
         <button
            type="button"
            class="btn-mini"
            data-testid={`${props.testid}-edit`}
            disabled={props.busy}
            onClick={(event) => {
               event.stopPropagation();
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
            onClick={(event) => {
               event.stopPropagation();
               props.onDuplicate();
            }}
         >
            Duplicate
         </button>
         <button
            type="button"
            class="btn-mini"
            data-testid={`${props.testid}-delete`}
            disabled={props.busy}
            onClick={(event) => {
               event.stopPropagation();
               props.onDelete();
            }}
         >
            Delete
         </button>
      </div>
   );
}
