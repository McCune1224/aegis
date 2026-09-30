import { createSignal, For, Show } from "solid-js";
import { previewSource, refreshSource, type CatalogEntry, type Source, type SourceInput, type SourcePreview } from "./api";
import DataTable, { type Column } from "./DataTable";
import Drawer from "./Drawer";

const formats = ["hosts", "domains", "adblock"];

type Props = {
  sources: Source[];
  catalog: CatalogEntry[];
  onSave: (name: string, input: SourceInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
  onReload: () => Promise<void>;
};

const staleAfter = 48 * 3_600_000;

// health summarizes one source for its badge: a failing source shows its
// streak, a stale one has not delivered a new body in a long time, and
// everything else is serving.
function health(source: Source): { label: string; kind: "allow" | "block" | "kind" } {
  if (source.failures > 0) {
    return { label: `failing ×${source.failures}`, kind: "block" };
  }
  if (source.enabled && source.last_fetch && Date.now() - new Date(source.last_fetch).getTime() > staleAfter) {
    return { label: "stale", kind: "kind" };
  }
  return { label: "ok", kind: "allow" };
}

function fetchTime(source: Source): string {
  if (!source.last_fetch) {
    return "never fetched";
  }
  const parsed = new Date(source.last_fetch);
  return Number.isNaN(parsed.getTime()) ? source.last_fetch : parsed.toLocaleString();
}

function schedule(source: Source): string {
  if (source.refresh_seconds <= 0) {
    return "";
  }
  const hours = source.refresh_seconds / 3600;
  if (hours >= 1 && Number.isInteger(hours)) {
    return `every ${hours}h`;
  }
  const minutes = Math.round(source.refresh_seconds / 60);
  if (minutes >= 1) {
    return `every ${minutes}m`;
  }
  return `every ${source.refresh_seconds}s`;
}

export default function Sources(props: Props) {
  const [name, setName] = createSignal("");
  const [url, setUrl] = createSignal("");
  const [format, setFormat] = createSignal("hosts");
  const [hours, setHours] = createSignal("");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);
  const [preview, setPreview] = createSignal<SourcePreview>();
  const [working, setWorking] = createSignal("");
  const [drawer, setDrawer] = createSignal(false);

  function pickCatalog(entry: string) {
    const known = props.catalog.find((item) => item.name === entry);
    if (!known) {
      return;
    }
    setName(known.name);
    setUrl(known.url);
    setFormat(known.format);
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (!name() || !url()) {
      setError("a source needs a name and a URL");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const seconds = Number(hours());
      await props.onSave(name(), {
        url: url(),
        format: format(),
        enabled: true,
        refresh_seconds: Number.isFinite(seconds) && seconds > 0 ? Math.round(seconds * 3600) : undefined,
      });
      setName("");
      setUrl("");
      setFormat("hosts");
      setHours("");
      setDrawer(false);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function toggle(source: Source, enabled: boolean) {
    setError(undefined);
    setPreview(undefined);
    try {
      await props.onSave(source.name, { enabled });
    } catch (cause) {
      setError(String(cause));
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

  async function refresh(name: string) {
    setWorking(name);
    setError(undefined);
    setPreview(undefined);
    try {
      await refreshSource(name);
      await props.onReload();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setWorking("");
    }
  }

  async function runPreview(name: string) {
    setWorking(name);
    setError(undefined);
    try {
      setPreview(await previewSource(name));
    } catch (cause) {
      setError(String(cause));
    } finally {
      setWorking("");
    }
  }

  const columns: Column<Source>[] = [
    {
      key: "name",
      label: "Source",
      sortable: true,
      value: (source) => source.name,
      render: (source) => (
        <div style={{ display: "flex", "flex-direction": "column", gap: "2px" }}>
          <span class="mono">{source.name}</span>
          <span class="selectors">{source.url}</span>
          <Show when={source.last_error}>
            <p class="error" data-testid="source-fetch-error" style={{ "margin-top": "4px" }}>
              {source.last_error}
            </p>
          </Show>
          <Show when={preview()?.name === source.name}>
            <div class="preview" data-testid="source-preview">
              <Show when={preview()?.notModified}>
                <p class="muted">the remote list has not changed</p>
              </Show>
              <Show when={!preview()?.notModified}>
                <p class="muted">a refresh would add {preview()?.added.length} and remove {preview()?.removed.length} domains</p>
                <Show when={(preview()?.added.length ?? 0) > 0}>
                  <span class="selectors">adding: {preview()?.added.join(", ")}</span>
                </Show>
                <Show when={(preview()?.removed.length ?? 0) > 0}>
                  <span class="selectors">removing: {preview()?.removed.join(", ")}</span>
                </Show>
              </Show>
            </div>
          </Show>
        </div>
      ),
    },
    {
      key: "format",
      label: "Format",
      sortable: true,
      value: (source) => source.format,
      render: (source) => <span class="badge kind">{source.format}</span>,
    },
    {
      key: "rules",
      label: "Rules",
      sortable: true,
      value: (source) => source.rule_count,
      render: (source) => (
        <span class="mono">
          {source.rule_count}
          <Show when={source.skipped > 0}>
            <span class="muted"> · {source.skipped} skipped</span>
          </Show>
        </span>
      ),
    },
    {
      key: "fetched",
      label: "Last fetch",
      sortable: true,
      value: (source) => source.last_fetch ?? "",
      render: (source) => (
        <span class="muted" style={{ "font-size": "11.5px" }}>
          {fetchTime(source)}
          {schedule(source) ? ` · ${schedule(source)}` : ""}
        </span>
      ),
    },
    {
      key: "health",
      label: "Health",
      value: (source) => health(source).label,
      render: (source) => (
        <span class={`badge ${health(source).kind}`} data-testid="source-health">
          {health(source).label}
        </span>
      ),
    },
    {
      key: "enabled",
      label: "On",
      value: (source) => (source.enabled ? 1 : 0),
      render: (source) => (
        <input
          type="checkbox"
          class="switch"
          data-testid="source-toggle"
          checked={source.enabled}
          onChange={(event) => void toggle(source, event.currentTarget.checked)}
        />
      ),
    },
    {
      key: "actions",
      label: "",
      value: () => "",
      render: (source) => (
        <div class="row-actions">
          <button
            type="button"
            class="btn-mini"
            disabled={working() === source.name}
            onClick={() => void runPreview(source.name)}
          >
            Preview
          </button>
          <button
            type="button"
            class="btn-mini"
            data-testid="source-refresh"
            disabled={working() === source.name}
            onClick={() => void refresh(source.name)}
          >
            Refresh
          </button>
          <button type="button" class="btn-mini" onClick={() => void remove(source.name)}>
            Delete
          </button>
        </div>
      ),
    },
  ];

  return (
    <>
      <div class="view">
        <div class="subbar">
          <h2 style={{ "font-size": "10.5px", "letter-spacing": "0.13em", "text-transform": "uppercase", color: "var(--text-2)", "font-weight": "650", "margin-right": "auto" }}>
            {props.sources.length} sources
          </h2>
          <button type="button" class="btn" data-testid="sources-new" onClick={() => setDrawer(true)}>
            + New source
          </button>
        </div>
        <Show when={error()}>
          <p class="alert-line error-line" role="alert">
            {error()}
          </p>
        </Show>
        <div style={{ flex: "1", "min-height": "0", overflow: "auto" }}>
          <DataTable
            columns={columns}
            rows={props.sources}
            rowKey={(source) => source.name}
            testid="source-rows"
            rowTestid={() => "source-row"}
            empty="no sources yet — add a blocklist below"
          />
        </div>
      </div>

      <Drawer open={drawer()} title="New source" onClose={() => setDrawer(false)}>
        <form onSubmit={(event) => void submit(event)} style={{ display: "contents" }}>
          <label>
            Known list
            <select data-testid="source-catalog" value="" onChange={(event) => pickCatalog(event.currentTarget.value)}>
              <option value="">or type a URL below</option>
              <For each={props.catalog}>{(entry) => <option value={entry.name}>{entry.name}</option>}</For>
            </select>
          </label>
          <label>
            Name
            <input data-testid="source-name" value={name()} onInput={(event) => setName(event.currentTarget.value)} />
          </label>
          <label>
            URL
            <input
              data-testid="source-url"
              value={url()}
              placeholder="https://example.com/list"
              onInput={(event) => setUrl(event.currentTarget.value)}
            />
          </label>
          <label>
            Format
            <select data-testid="source-format" value={format()} onInput={(event) => setFormat(event.currentTarget.value)}>
              <For each={formats}>{(value) => <option value={value}>{value}</option>}</For>
            </select>
          </label>
          <label>
            Refresh every (hours, blank uses the default schedule)
            <input
              data-testid="source-refresh-hours"
              value={hours()}
              placeholder="6"
              inputmode="numeric"
              onInput={(event) => setHours(event.currentTarget.value)}
            />
          </label>
          {error() ? <p class="error">{error()}</p> : null}
          <div class="row-actions" style={{ "justify-content": "flex-end" }}>
            <button type="submit" class="btn" data-testid="source-save" disabled={busy()}>
              Save
            </button>
          </div>
        </form>
      </Drawer>
    </>
  );
}
