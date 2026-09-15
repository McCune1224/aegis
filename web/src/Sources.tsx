import { createSignal, For, Show } from "solid-js";
import type { CatalogEntry, Source, SourceInput } from "./api";

const formats = ["hosts", "domains", "adblock"];

type Props = {
  sources: Source[];
  catalog: CatalogEntry[];
  onSave: (name: string, input: SourceInput) => Promise<void>;
  onDelete: (name: string) => Promise<void>;
};

function fetchTime(source: Source): string {
  if (!source.last_fetch) {
    return "never fetched";
  }
  const parsed = new Date(source.last_fetch);
  return Number.isNaN(parsed.getTime()) ? source.last_fetch : parsed.toLocaleString();
}

export default function Sources(props: Props) {
  const [name, setName] = createSignal("");
  const [url, setUrl] = createSignal("");
  const [format, setFormat] = createSignal("hosts");
  const [error, setError] = createSignal<string>();
  const [busy, setBusy] = createSignal(false);

  function pickCatalog(entry: string) {
    const known = props.catalog.find((item) => item.name === entry);
    if (!known) {
      return;
    }
    setName(known.name);
    setUrl(known.url);
    setFormat(known.format);
  }

  function reset() {
    setName("");
    setUrl("");
    setFormat("hosts");
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
      await props.onSave(name(), { url: url(), format: format(), enabled: true });
      reset();
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function toggle(source: Source, enabled: boolean) {
    setError(undefined);
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

  return (
    <section>
      <ul>
        <For each={props.sources}>
          {(source) => (
            <li data-testid="source-row">
              <div>
                <strong>{source.name}</strong>
                <span class="muted">
                  {source.format} {source.rule_count} rules {fetchTime(source)}
                </span>
                <span class="selectors">{source.url}</span>
                <Show when={source.last_error}>
                  <p class="error" data-testid="source-fetch-error">
                    {source.last_error}
                  </p>
                </Show>
              </div>
              <div class="row-actions">
                <label>
                  <input
                    type="checkbox"
                    data-testid="source-toggle"
                    checked={source.enabled}
                    onChange={(event) => void toggle(source, event.currentTarget.checked)}
                  />
                  enabled
                </label>
                <button type="button" onClick={() => void remove(source.name)}>
                  Delete
                </button>
              </div>
            </li>
          )}
        </For>
      </ul>

      <form onSubmit={(event) => void submit(event)}>
        <h2>New source</h2>
        <label>
          Known list
          <select
            data-testid="source-catalog"
            value=""
            onChange={(event) => pickCatalog(event.currentTarget.value)}
          >
            <option value="">or type a URL below</option>
            <For each={props.catalog}>{(entry) => <option value={entry.name}>{entry.name}</option>}</For>
          </select>
        </label>
        <label>
          Name
          <input
            data-testid="source-name"
            value={name()}
            onInput={(event) => setName(event.currentTarget.value)}
          />
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
          <select
            data-testid="source-format"
            value={format()}
            onInput={(event) => setFormat(event.currentTarget.value)}
          >
            <For each={formats}>{(value) => <option value={value}>{value}</option>}</For>
          </select>
        </label>
        <div class="row-actions">
          <button type="submit" data-testid="source-save" disabled={busy()}>
            Save
          </button>
        </div>
        {error() ? <p class="error">{error()}</p> : null}
      </form>
    </section>
  );
}
