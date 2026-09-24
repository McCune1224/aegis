import { createMemo, createSignal, For, Show } from "solid-js";
import type { BlockedService } from "./api";
import { groupServices } from "./services";

// Scope names the layer the sliders on this page edit: a profile blocks a
// service for every client on it, a client blocks one for itself alone. The
// layers add up, so a service blocked by a profile stays blocked for a client
// that has not enabled it here.
export type ServiceScope = { kind: "profile" | "client"; name: string };

type Props = {
  services: BlockedService[];
  serviceGroups: string[];
  profileNames: string[];
  clientNames: string[];
  defaultProfile: string;
  onSave: (scope: ServiceScope, services: string[]) => Promise<void>;
  onRefreshServices: () => Promise<void>;
};

export default function BlockedServices(props: Props) {
  const [scope, setScope] = createSignal<ServiceScope>({
    kind: "profile",
    name: props.defaultProfile || props.profileNames[0] || "",
  });
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string>();

  const grouped = createMemo(() => groupServices(props.services, props.serviceGroups));

  const enabled = createMemo(() => {
    const current = scope();
    return props.services
      .filter((service) => (current.kind === "profile" ? service.profiles : service.clients).includes(current.name))
      .map((service) => service.id);
  });

  function pick(kind: "profile" | "client", name: string) {
    setScope({ kind, name });
    setError(undefined);
  }

  async function apply(next: string[]) {
    const current = scope();
    if (!current.name) {
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      await props.onSave(current, next);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  const toggle = (id: string) => apply(toggledSet(enabled(), [id], true));
  const blockAll = (ids: string[]) => apply(toggledSet(enabled(), ids, true));
  const unblockAll = (ids: string[]) => apply(toggledSet(enabled(), ids, false));

  async function refreshCatalog() {
    setError(undefined);
    try {
      await props.onRefreshServices();
    } catch (cause) {
      setError(String(cause));
    }
  }

  return (
    <section class="panel blocked-services">
      <div class="blocked-services-head">
        <p class="muted">Allows to quickly block popular sites and services.</p>
        <label class="scope-picker">
          Apply to
          <select
            data-testid="services-scope"
            value={`${scope().kind}:${scope().name}`}
            disabled={busy()}
            onChange={(event) => {
              const value = event.currentTarget.value;
              const cut = value.indexOf(":");
              pick(value.slice(0, cut) as "profile" | "client", value.slice(cut + 1));
            }}
          >
            <optgroup label="Profiles">
              <For each={props.profileNames}>
                {(name) => <option value={`profile:${name}`}>{name}</option>}
              </For>
            </optgroup>
            <optgroup label="Clients">
              <For each={props.clientNames}>
                {(name) => <option value={`client:${name}`}>{name}</option>}
              </For>
            </optgroup>
          </select>
        </label>
      </div>

      <Show when={scope().kind === "client" && scope().name}>
        <p class="muted scope-note" data-testid="client-scope-note">
          These toggles add to what the {scope().name} profile already blocks.
        </p>
      </Show>

      <div class="blocked-services-actions">
        <button type="button" data-testid="services-block-all" disabled={busy()} onClick={() => blockAll(props.services.map((service) => service.id))}>
          Block all
        </button>
        <button type="button" data-testid="services-unblock-all" disabled={busy()} onClick={() => unblockAll(props.services.map((service) => service.id))}>
          Unblock all
        </button>
      </div>

      <Show when={props.services.length === 0}>
        <p class="muted">No services in the catalog yet.</p>
      </Show>

      <For each={grouped()}>
        {(group) => (
          <div class="service-section">
            <div class="service-section-head">
              <h2>{group.group || "Other"}</h2>
              <button type="button" class="link block-link" disabled={busy()} onClick={() => blockAll(group.services.map((service) => service.id))}>
                Block all
              </button>
              <button type="button" class="link unblock-link" disabled={busy()} onClick={() => unblockAll(group.services.map((service) => service.id))}>
                Unblock all
              </button>
            </div>
            <div class="service-grid">
              <For each={group.services}>
                {(service) => (
                  <label class="service-card">
                    <Show when={service.icon_svg} fallback={<span class="service-icon service-icon-fallback">{service.name[0]}</span>}>
                      <span class="service-icon" innerHTML={service.icon_svg} />
                    </Show>
                    <span class="service-name">{service.name}</span>
                    <input
                      type="checkbox"
                      class="switch"
                      data-testid={`service-${service.id}`}
                      checked={enabled().includes(service.id)}
                      disabled={busy()}
                      onChange={() => void toggle(service.id)}
                    />
                  </label>
                )}
              </For>
            </div>
          </div>
        )}
      </For>

      <div class="row-actions">
        <button type="button" data-testid="services-refresh" onClick={() => void refreshCatalog()}>
          Refresh catalog
        </button>
      </div>
      {error() ? <p class="error">{error()}</p> : null}
    </section>
  );
}

// toggledSet returns the set with every id in names added when on is true and
// removed when it is false, which is the whole body the endpoints replace.
function toggledSet(current: string[], ids: string[], on: boolean): string[] {
  if (!on) {
    return current.filter((id) => !ids.includes(id));
  }
  return [...new Set([...current, ...ids])];
}
