import { createSignal, For, Show } from "solid-js";
import type { Rewrite } from "./api";
import DataTable, { type Column } from "./DataTable";
import Drawer from "./Drawer";

type Props = {
  rewrites: Rewrite[];
  onSave: (pattern: string, target: string) => Promise<void>;
  onDelete: (pattern: string) => Promise<void>;
};

export default function Rewrites(props: Props) {
  const [pattern, setPattern] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);
  const [drawer, setDrawer] = createSignal(false);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!pattern().trim()) {
      setError("a rewrite needs a pattern");
      return;
    }
    if (!target().trim()) {
      setError("a rewrite needs a target");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(pattern().trim(), target().trim());
      setPattern("");
      setTarget("");
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
    } catch (cause) {
      setError(String(cause));
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
        <button type="button" class="btn-mini" data-testid="rewrite-delete" onClick={() => void remove(rewrite.pattern)}>
          Delete
        </button>
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
          <button type="button" class="btn" data-testid="rewrites-new" onClick={() => setDrawer(true)}>
            + New rewrite
          </button>
        </div>
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

      <Drawer open={drawer()} title="New rewrite" onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
          <label>
            Name
            <input
              data-testid="rewrite-pattern"
              value={pattern()}
              placeholder="nas.local or *.nas.local"
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
          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="rewrite-save" disabled={busy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
