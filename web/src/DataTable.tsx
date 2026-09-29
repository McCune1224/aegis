import { createMemo, createSignal, For, Show } from "solid-js";
import type { JSX } from "@solidjs/web";

export type Column<T> = {
  key: string;
  label: string;
  value: (row: T) => string | number;
  render?: (row: T) => JSX.Element;
  sortable?: boolean;
};

type Props<T> = {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string | number;
  rowClass?: (row: T) => string | undefined;
  onRowClick?: (row: T) => void;
  testid?: string;
  rowTestid?: (row: T) => string | undefined;
  empty?: string;
};

// The one list surface. Sortable where a column declares a value; rows are
// plain table rows, separators live on the tr (see app.css).
export default function DataTable<T>(props: Props<T>) {
  const [sortKey, setSortKey] = createSignal<string>();
  const [direction, setDirection] = createSignal<1 | -1>(1);

  const sorted = createMemo(() => {
    const column = props.columns.find((candidate) => candidate.key === sortKey());
    if (!column) {
      return props.rows;
    }
    return [...props.rows].sort((a, b) => {
      const left = column.value(a);
      const right = column.value(b);
      if (typeof left === "number" && typeof right === "number") {
        return (left - right) * direction();
      }
      return String(left).localeCompare(String(right)) * direction();
    });
  });

  const toggle = (key: string) => {
    if (sortKey() === key) {
      setDirection((current) => (current === 1 ? -1 : 1));
    } else {
      setSortKey(key);
      setDirection(1);
    }
  };

  return (
    <div class="tbl-wrap">
      <table class="tbl" data-testid={props.testid}>
        <thead>
          <tr>
            <For each={props.columns}>
              {(column) => (
                <th class={column.sortable ? "sortable" : undefined}>
                  <Show
                    when={column.sortable}
                    fallback={<span>{column.label}</span>}
                  >
                    <button type="button" onClick={() => toggle(column.key)}>
                      {column.label}
                      <Show when={sortKey() === column.key}>
                        <span class="sort-arrow">{direction() === 1 ? "▲" : "▼"}</span>
                      </Show>
                    </button>
                  </Show>
                </th>
              )}
            </For>
          </tr>
        </thead>
        <tbody>
          <For each={sorted()}>
            {(row) => (
              <tr
                data-testid={props.rowTestid?.(row)}
                class={[
                  props.onRowClick ? "clickable" : "",
                  props.rowClass?.(row) ?? "",
                ]
                  .filter(Boolean)
                  .join(" ") || undefined}
                onClick={() => props.onRowClick?.(row)}
              >
                <For each={props.columns}>
                  {(column) => (
                    <td>{column.render ? column.render(row) : column.value(row)}</td>
                  )}
                </For>
              </tr>
            )}
          </For>
          <Show when={props.rows.length === 0}>
            <tr>
              <td colspan={props.columns.length} class="empty">
                {props.empty ?? "nothing here yet"}
              </td>
            </tr>
          </Show>
        </tbody>
      </table>
    </div>
  );
}
