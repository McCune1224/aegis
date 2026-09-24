import { createMemo, createSignal, For, Show } from "solid-js";
import type { BlockedService, Client } from "./api";
import { groupServices, serviceGroupLabel, toggled } from "./services";

// Scope names the layer the sliders on this page edit: a profile blocks a
// service for every client on it, a client blocks one for itself alone. The
// layers add up, so a service blocked by a profile stays blocked for a client
// that has not enabled it here.
export type ServiceScope = { kind: "profile" | "client"; name: string };

type Props = {
  services: BlockedService[];
  serviceGroups: string[];
  profileNames: string[];
  clients: Client[];
  defaultProfile: string;
  onSave: (scope: ServiceScope, services: string[]) => Promise<void>;
  onRefreshServices: () => Promise<void>;
};

export default function BlockedServices(props: Props) {
  const [scope, setScope] = createSignal<ServiceScope>({
    kind: "profile",
    name: props.defaultProfile || props.profileNames[0] || "",
  });
  const [search, setSearch] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [saved, setSaved] = createSignal(false);
  const [error, setError] = createSignal<string>();

  const grouped = createMemo(() => groupServices(props.services, props.serviceGroups));

  const clientProfile = (name: string) =>
    props.clients.find((client) => client.name === name)?.profile ?? "";

  // editableIds is the set this scope's own sliders control.
  const editableIds = createMemo(() => {
    const current = scope();
    return new Set(
      props.services
        .filter((service) =>
          current.kind === "profile" ? service.profiles.includes(current.name) : service.clients.includes(current.name),
        )
        .map((service) => service.id),
    );
  });

  // inheritedIds is what already blocks the viewed client through its profile.
  const inheritedIds = createMemo(() => {
    const current = scope();
    if (current.kind !== "client") {
      return new Set<string>();
    }
    const profile = clientProfile(current.name);
    return new Set(
      props.services.filter((service) => service.profiles.includes(profile)).map((service) => service.id),
    );
  });

  const matches = (service: BlockedService) =>
    search() === "" ||
    service.name.toLowerCase().includes(search().toLowerCase()) ||
    service.id.toLowerCase().includes(search().toLowerCase());

  // cardState is read inside JSX expressions, not destructured beforehand, so
  // every read is tracked and a scope change re-renders the cards.
  const cardState = (service: BlockedService) => {
    const own = editableIds().has(service.id);
    return { own, inherited: !own && inheritedIds().has(service.id) };
  };

  const visibleGroups = createMemo(() =>
    grouped()
      .map((group) => ({ ...group, services: group.services.filter(matches) }))
      .filter((group) => group.services.length > 0),
  );

  function pick(kind: "profile" | "client", name: string) {
    setScope({ kind, name });
    setSaved(false);
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
      setSaved(true);
    } catch (cause) {
      setError(String(cause));
    } finally {
      setBusy(false);
    }
  }

  // toggle flips one service in the set this scope owns. The switch's own
  // state decides the direction: checked means the click removes it.
  const toggle = (id: string) => apply(toggled([...editableIds()], id));
  const blockAll = (ids: string[]) => apply([...new Set([...editableIds(), ...ids])]);
  const unblockAll = (ids: string[]) => apply([...editableIds()].filter((id) => !ids.includes(id)));

  async function refreshCatalog() {
    setError(undefined);
    try {
      await props.onRefreshServices();
    } catch (cause) {
      setError(String(cause));
    }
  }

  const inheritedCount = () => [...inheritedIds()].filter((id) => !editableIds().has(id)).length;

  return (
    <section class="panel blocked-services">
      <div class="blocked-services-head">
        <p class="muted">Block popular online services for one profile or one client.</p>
        <div class="scope-picker">
          <div class="seg" role="tablist">
            <button
              type="button"
              class={`seg-btn${scope().kind === "profile" ? " active" : ""}`}
              disabled={busy()}
              onClick={() => pick("profile", scope().kind === "profile" ? scope().name : props.defaultProfile || props.profileNames[0] || "")}
            >
              Whole profile
            </button>
            <button
              type="button"
              class={`seg-btn${scope().kind === "client" ? " active" : ""}`}
              disabled={busy()}
              onClick={() => pick("client", scope().kind === "client" ? scope().name : props.clients[0]?.name || "")}
            >
              Single client
            </button>
          </div>
          <Show
            when={scope().kind === "client"}
            fallback={
              <select
                data-testid="services-scope"
                value={scope().name}
                disabled={busy()}
                onChange={(event) => pick("profile", event.currentTarget.value)}
              >
                <For each={props.profileNames}>{(name) => <option value={name}>{name}</option>}</For>
              </select>
            }
          >
            <select
              data-testid="services-scope"
              value={scope().name}
              disabled={busy()}
              onChange={(event) => pick("client", event.currentTarget.value)}
            >
              <For each={props.clients}>{(client) => <option value={client.name}>{client.name}</option>}</For>
            </select>
          </Show>
        </div>
      </div>

      <div class="blocked-services-summary">
        <Show
          when={scope().kind === "client"}
          fallback={
            <p class="muted" data-testid="scope-note">
              These services are blocked for every client on <strong>{scope().name}</strong>.
            </p>
          }
        >
          <p class="muted" data-testid="scope-note">
            Blocked for <strong>{scope().name}</strong> only, on top of its profile.{" "}
            <Show when={inheritedCount() > 0}>
              {inheritedCount()} more come from the <strong>{clientProfile(scope().name)}</strong> profile and show as
              locked.
            </Show>
          </p>
        </Show>
        <Show when={saved() && !busy()}>
          <span class="badge saved" data-testid="services-saved">
            saved
          </span>
        </Show>
      </div>

      <div class="blocked-services-toolbar">
        <input
          type="search"
          class="service-search"
          placeholder="Search services…"
          data-testid="services-search"
          value={search()}
          onInput={(event) => setSearch(event.currentTarget.value)}
        />
        <div class="blocked-services-actions">
          <button type="button" data-testid="services-block-all" disabled={busy()} onClick={() => blockAll(props.services.map((service) => service.id))}>
            Block all
          </button>
          <button type="button" data-testid="services-unblock-all" disabled={busy()} onClick={() => unblockAll(props.services.map((service) => service.id))}>
            Unblock all
          </button>
        </div>
      </div>

      <Show when={props.services.length === 0}>
        <p class="muted">No services in the catalog yet.</p>
      </Show>

      <For each={visibleGroups()}>
        {(group) => (
          <div class="service-section">
            <div class="service-section-head">
              <h2>{serviceGroupLabel(group.group)}</h2>
              <span class="muted service-count">
                {group.services.filter((service) => editableIds().has(service.id) || inheritedIds().has(service.id)).length}
                /
                {group.services.length}
              </span>
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
                  <label
                    class={`service-card${cardState(service).inherited ? " inherited" : ""}`}
                    title={cardState(service).inherited ? `Blocked by the ${clientProfile(scope().name)} profile` : ""}
                  >
                    <Show when={service.icon_svg} fallback={<span class="service-icon service-icon-fallback">{service.name[0]}</span>}>
                      <span class="service-icon" innerHTML={service.icon_svg} />
                    </Show>
                    <span class="service-name">{service.name}</span>
                    <Show when={cardState(service).inherited}>
                      <span class="badge profile-badge">profile</span>
                    </Show>
                    <input
                      type="checkbox"
                      class="switch"
                      data-testid={`service-${service.id}`}
                      checked={cardState(service).own || cardState(service).inherited}
                      disabled={busy() || cardState(service).inherited}
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
