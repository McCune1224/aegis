import type { JSX } from "@solidjs/web";
import { createEffect, createSignal, For, Show } from "solid-js";
import { appHashFor, parseAppHash } from "./hash";
import {
  createRoute as postRoute,
  createRule as postRule,
  deleteClient as removeClient,
  deleteProfile as removeProfile,
  deleteRewrite as removeRewrite,
  deleteRoute as removeRoute,
  deleteRule as removeRule,
  deleteSchedule as removeSchedule,
  deleteSource as removeSource,
  deleteUpstream as removeUpstream,
  getAccess,
  getDefaultProfile,
  getStatus,
  listCatalog,
  listClients,
  listObserved,
  listProfiles,
  listRewrites,
  listRoutes,
  listRules,
  listWindows,
  listSchedules,
  listSafesearch,
  listDiscoveries,
  dismissDiscovery as removeDiscovery,
  listThreatFindings,
  listServices,
  listSources,
  listUpstreams,
  refreshServices as postRefreshServices,
  saveClient as putClient,
  saveClientServices as putClientServices,
  saveProfile as putProfile,
  saveProfileSafesearch as putProfileSafesearch,
  saveProfileServices as putProfileServices,
  saveRewrite as putRewrite,
  saveWindow as putWindow,
  deleteWindow as deleteWindowByName,
  saveSchedule as putSchedule,
  saveSource as putSource,
  saveUpstream as putUpstream,
  saveAccess as putAccess,
  setDefaultProfile as putDefaultProfile,
  updateRoute as patchRoute,
  updateRule as patchRule,
  type CatalogEntry,
  type Client,
  type ClientInput,
  type Profile,
  type ProfileInput,
  type BlockedService,
  type Discovery,
  type Observed,
  type SafesearchEngine,
  type ThreatFinding,
  type Rewrite,
  type Route,
  type RouteInput,
  type Rule,
  type ServiceWindow,
  type ServiceWindowInput,
  type RuleInput,
  type Schedule,
  type ScheduleInput,
  type Source,
  type SourceInput,
  type Upstream,
  type UpstreamInput,
  type AccessSettings,
} from "./api";
import BlockedServices, { type ServiceScope } from "./BlockedServices";
import Clients from "./Clients";
import Dashboard from "./Dashboard";
import Graph from "./Graph";
import QueryLog from "./QueryLog";
import Profiles from "./Profiles";
import Rewrites from "./Rewrites";
import Rules from "./Rules";
import Sources from "./Sources";
import Upstreams from "./Upstreams";
import Settings from "./Settings";
import { applyServiceScope, dropBy, upsertBy } from "./rows";
import { seenDevices, type Device } from "./topology";
import { createQueryLog } from "./querylog";

export type Tab =
  | "dashboard"
  | "constellation"
  | "log"
  | "clients"
  | "profiles"
  | "rules"
  | "rewrites"
  | "services"
  | "system";

export type SystemView = "upstreams" | "sources" | "settings";

const RAIL: { id: Tab; label: string }[] = [
  { id: "dashboard", label: "Overview" },
  { id: "constellation", label: "Constellation" },
  { id: "log", label: "Query Log" },
  { id: "clients", label: "Clients" },
  { id: "profiles", label: "Profiles" },
  { id: "rules", label: "Rules" },
  { id: "rewrites", label: "Rewrites" },
  { id: "services", label: "Blocked Services" },
  { id: "system", label: "System" },
];

const VIEW_NAMES: Record<Tab, string> = {
  dashboard: "Overview",
  constellation: "Constellation",
  log: "Query Log",
  clients: "Clients",
  profiles: "Profiles",
  rules: "Rules",
  rewrites: "Rewrites",
  services: "Blocked Services",
  system: "System",
};

const SYSTEM_VIEWS: { id: SystemView; label: string }[] = [
  { id: "upstreams", label: "Upstreams & routes" },
  { id: "sources", label: "Block sources" },
  { id: "settings", label: "Settings" },
];

export default function App() {
  const [tab, setTab] = createSignal<Tab>(parseAppHash(location.hash).tab);
  const [systemView, setSystemView] = createSignal<SystemView>(parseAppHash(location.hash).system);
  const [profiles, setProfiles] = createSignal<Profile[]>([]);
  const [clients, setClients] = createSignal<Client[]>([]);
  const [sources, setSources] = createSignal<Source[]>([]);
  const [catalog, setCatalog] = createSignal<CatalogEntry[]>([]);
  const [services, setServices] = createSignal<BlockedService[]>([]);
  const [serviceGroups, setServiceGroups] = createSignal<string[]>([]);
  const [safesearch, setSafesearch] = createSignal<SafesearchEngine[]>([]);
  const [discoveries, setDiscoveries] = createSignal<Discovery[]>([]);
  const [observed, setObserved] = createSignal<Observed[]>([]);
  const [threats, setThreats] = createSignal<ThreatFinding[]>([]);
  const [rules, setRules] = createSignal<Rule[]>([]);
  const [schedules, setSchedules] = createSignal<Schedule[]>([]);
  const [windows, setWindows] = createSignal<ServiceWindow[]>([]);
  const [rewrites, setRewrites] = createSignal<Rewrite[]>([]);
  const [upstreamRows, setUpstreamRows] = createSignal<Upstream[]>([]);
  const [routes, setRoutes] = createSignal<Route[]>([]);
  const [access, setAccess] = createSignal<AccessSettings>({ allowed: [], disallowed: [] });
  const [defaultProfile, setDefaultProfile] = createSignal("");
  const [upstreams, setUpstreams] = createSignal<string[]>([]);
  const [ruleCount, setRuleCount] = createSignal(0);
  const [error, setError] = createSignal<string>();
  const [windowMinutes, setWindowMinutes] = createSignal(Number(localStorage.getItem("aegis.window")) || 60);
  const [live, setLive] = createSignal(localStorage.getItem("aegis.live") !== "0");
  const [logFilter, setLogFilter] = createSignal<{ client?: string; name?: string }>({});
  const [paletteOpen, setPaletteOpen] = createSignal(false);
  const [paletteQuery, setPaletteQuery] = createSignal("");
  const [paletteIndex, setPaletteIndex] = createSignal(0);

  const log = createQueryLog({ live: live() });

  createEffect(
    () => appHashFor(tab(), systemView()),
    (next) => {
      if (location.hash !== next) {
        history.replaceState(null, "", next);
      }
    },
  );

  const paletteItems = () => {
    const items: { label: string; hint: string; keywords: string; run: () => void }[] = RAIL.map((item) => ({
      label: item.label,
      hint: "view",
      keywords: item.label.toLowerCase(),
      run: () => setTab(item.id),
    }));
    items.push({
      label: live() ? "Stop the live query stream" : "Start the live query stream",
      hint: "action",
      keywords: "live stream toggle pause resume",
      run: () => setLive((current) => !current),
    });
    items.push({
      label: "Refresh the services catalog",
      hint: "action",
      keywords: "services catalog refresh",
      run: () => {
        void postRefreshServices()
          .then(() => refresh())
          .catch((cause) => setError(String(cause)));
      },
    });
    const query = paletteQuery().trim().toLowerCase();
    if (!query) {
      return items;
    }
    return items.filter((item) => item.keywords.includes(query) || item.label.toLowerCase().includes(query));
  };

  function openPalette() {
    setPaletteQuery("");
    setPaletteIndex(0);
    setPaletteOpen(true);
  }

  function runPaletteItem(index: number) {
    const item = paletteItems()[index];
    if (!item) {
      return;
    }
    setPaletteOpen(false);
    item.run();
  }

  function onPaletteKey(event: KeyboardEvent) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setPaletteIndex((index) => Math.min(index + 1, paletteItems().length - 1));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setPaletteIndex((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      event.preventDefault();
      runPaletteItem(paletteIndex());
    } else if (event.key === "Escape") {
      setPaletteOpen(false);
    }
  }

  createEffect(
    () => undefined,
    () => {
      const onKey = (event: KeyboardEvent) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
          event.preventDefault();
          if (paletteOpen()) {
            setPaletteOpen(false);
          } else {
            openPalette();
          }
        }
      };
      window.addEventListener("keydown", onKey);
      return () => window.removeEventListener("keydown", onKey);
    },
  );

  async function refresh() {
    const [nextProfiles, nextClients, nextSources, nextCatalog, nextRules, nextSchedules, nextWindows, nextRewrites, nextUpstreams, nextRoutes, nextAccess, nextDefault, nextStatus, nextServices, nextSafesearch, nextDiscoveries, nextThreats, nextObserved] =
      await Promise.all([
        listProfiles(),
        listClients(),
        listSources(),
        listCatalog(),
        listRules(),
        listSchedules(),
        listWindows(),
        listRewrites(),
        listUpstreams(),
        listRoutes(),
        getAccess(),
        getDefaultProfile(),
        getStatus(),
        listServices(),
        listSafesearch(),
        listDiscoveries(),
        listThreatFindings(),
        listObserved(),
      ]);
    setProfiles(nextProfiles);
    setClients(nextClients);
    setSources(nextSources);
    setCatalog(nextCatalog);
    setRules(nextRules);
    setSchedules(nextSchedules);
    setWindows(nextWindows);
    setRewrites(nextRewrites);
    setUpstreamRows(nextUpstreams);
    setRoutes(nextRoutes);
    setAccess(nextAccess);
    setDefaultProfile(nextDefault.profile);
    setUpstreams(nextStatus.upstreams);
    setRuleCount(nextStatus.rules ?? 0);
    setServices(nextServices.services);
    setServiceGroups(nextServices.groups);
    setSafesearch(nextSafesearch.engines);
    setDiscoveries(nextDiscoveries.discoveries);
    setThreats(nextThreats.findings);
    setObserved(nextObserved);
  }

  createEffect(
    () => undefined,
    () => {
      void refresh().catch((cause) => setError(String(cause)));
      void log.load({ limit: 2000 }).catch((cause) => setError(String(cause)));
    },
  );

  // Each fetcher owns one collection: fetch it and set it, nothing else. A
  // write applies its own response to the list at once, then reload names the
  // collections the write can also move and runs them in the background, so a
  // reload that fails is an error on screen instead of a silent stale list.
  const FETCHERS = {
    profiles: async () => setProfiles(await listProfiles()),
    clients: async () => setClients(await listClients()),
    sources: async () => setSources(await listSources()),
    rules: async () => setRules(await listRules()),
    schedules: async () => setSchedules(await listSchedules()),
    windows: async () => setWindows(await listWindows()),
    rewrites: async () => setRewrites(await listRewrites()),
    upstreams: async () => setUpstreamRows(await listUpstreams()),
    routes: async () => setRoutes(await listRoutes()),
    access: async () => setAccess(await getAccess()),
    safesearch: async () => setSafesearch((await listSafesearch()).engines),
    discoveries: async () => setDiscoveries((await listDiscoveries()).discoveries),
    observed: async () => setObserved(await listObserved()),
    services: async () => {
      const page = await listServices();
      setServices(page.services);
      setServiceGroups(page.groups);
    },
    status: async () => {
      const status = await getStatus();
      setUpstreams(status.upstreams);
      setRuleCount(status.rules ?? 0);
    },
  };
  type Collection = keyof typeof FETCHERS;

  function reload(...names: Collection[]): Promise<void> {
    return Promise.all(names.map((name) => FETCHERS[name]())).then(
      () => undefined,
      (cause) => {
        setError(`saved, but reloading the lists failed: ${String(cause)}`);
      },
    );
  }

  async function saveProfile(name: string, input: ProfileInput) {
    const saved = await putProfile(name, input);
    setProfiles((rows) => upsertBy(rows, saved, (row) => row.name));
  }

  async function saveClient(name: string, input: ClientInput) {
    const saved = await putClient(name, input);
    setClients((rows) => upsertBy(rows, saved, (row) => row.name));
    reload("observed", "discoveries");
  }

  async function deleteProfile(name: string) {
    await removeProfile(name);
    setProfiles((rows) => dropBy(rows, (row) => row.name, name));
    reload("clients");
  }

  async function deleteClient(name: string) {
    await removeClient(name);
    setClients((rows) => dropBy(rows, (row) => row.name, name));
    reload("observed", "discoveries");
  }

  async function saveSource(name: string, input: SourceInput) {
    const saved = await putSource(name, input);
    setSources((rows) => upsertBy(rows, saved, (row) => row.name));
  }

  async function deleteSource(name: string) {
    await removeSource(name);
    setSources((rows) => dropBy(rows, (row) => row.name, name));
    reload("status");
  }

  async function addRule(input: RuleInput) {
    const saved = await postRule(input);
    setRules((rows) => upsertBy(rows, saved, (row) => String(row.id)));
    reload("status");
  }

  async function changeRule(id: number, input: RuleInput) {
    const saved = await patchRule(id, input);
    setRules((rows) => upsertBy(rows, saved, (row) => String(row.id)));
    reload("status");
  }

  async function deleteRule(id: number) {
    await removeRule(id);
    setRules((rows) => dropBy(rows, (row) => String(row.id), String(id)));
    reload("status");
  }

  async function addSchedule(name: string, input: ScheduleInput) {
    const saved = await putSchedule(name, input);
    setSchedules((rows) => upsertBy(rows, saved, (row) => row.name));
  }

  async function deleteSchedule(name: string) {
    await removeSchedule(name);
    setSchedules((rows) => dropBy(rows, (row) => row.name, name));
    reload("windows");
  }

  async function addWindow(name: string, input: ServiceWindowInput) {
    const saved = await putWindow(name, input);
    setWindows((rows) => upsertBy(rows, saved, (row) => row.name));
  }

  async function deleteServiceWindow(name: string) {
    await deleteWindowByName(name);
    setWindows((rows) => dropBy(rows, (row) => row.name, name));
  }

  async function saveRewrite(pattern: string, target: string) {
    const saved = await putRewrite(pattern, target);
    setRewrites((rows) => upsertBy(rows, saved, (row) => row.pattern));
  }

  async function deleteRewrite(pattern: string) {
    await removeRewrite(pattern);
    setRewrites((rows) => dropBy(rows, (row) => row.pattern, pattern));
  }

  async function saveUpstream(name: string, input: UpstreamInput) {
    const saved = await putUpstream(name, input);
    setUpstreamRows((rows) => upsertBy(rows, saved, (row) => row.name));
    reload("status");
  }

  async function deleteUpstream(name: string) {
    await removeUpstream(name);
    setUpstreamRows((rows) => dropBy(rows, (row) => row.name, name));
    reload("status", "routes");
  }

  async function addRoute(input: RouteInput) {
    const saved = await postRoute(input);
    setRoutes((rows) => upsertBy(rows, saved, (row) => String(row.id)));
  }

  async function changeRoute(id: number, input: RouteInput) {
    const saved = await patchRoute(id, input);
    setRoutes((rows) => upsertBy(rows, saved, (row) => String(row.id)));
  }

  async function deleteRoute(id: number) {
    await removeRoute(id);
    setRoutes((rows) => dropBy(rows, (row) => String(row.id), String(id)));
  }

  async function makeDefault(name: string) {
    const saved = await putDefaultProfile(name);
    setDefaultProfile(saved.profile);
  }

  async function saveClientServices(scope: ServiceScope, serviceIDs: string[]) {
    const save = scope.kind === "client" ? putClientServices : putProfileServices;
    await save(scope.name, serviceIDs);
    setServices((rows) => applyServiceScope(rows, scope.kind, scope.name, serviceIDs));
  }

  async function refreshServiceCatalog() {
    await postRefreshServices();
    await FETCHERS.services();
  }

  async function saveProfileSafesearch(name: string, engines: string[]) {
    await putProfileSafesearch(name, engines);
    const saved = new Set(engines);
    setSafesearch((rows) =>
      rows.map((row) => {
        const others = row.profiles.filter((entry) => entry !== name);
        return { ...row, profiles: saved.has(row.id) ? [...others, name].sort() : others };
      }),
    );
  }

  async function claimObserved(entry: Observed) {
    await saveClient(entry.client, {
      profile: defaultProfile(),
      notes: `claimed after ${entry.queries} queries`,
      addresses: [entry.client],
      macs: [],
      prefixes: [],
    });
  }

  async function claimDiscovery(discovery: Discovery, name: string) {
    await saveClient(name, {
      profile: defaultProfile(),
      notes: `discovered at ${discovery.address}`,
      addresses: [discovery.address],
      macs: [discovery.mac],
      prefixes: [],
    });
    await removeDiscovery(discovery.mac);
    setDiscoveries((rows) => dropBy(rows, (row) => row.mac, discovery.mac));
  }

  // claimDevice routes a graph claim through the feed the sighting came from:
  // a DHCP discovery keeps its hardware address, a query-log address claims by
  // what the resolver saw.
  async function claimDevice(device: Device, name: string) {
    const discovery = device.mac ? discoveries().find((entry) => entry.mac === device.mac) : undefined;
    if (discovery) {
      await claimDiscovery(discovery, name);
      return;
    }
    const entry = observed().find((seen) => seen.client === device.address);
    await claimObserved(
      entry ?? { client: device.address, queries: 0, last_seen: Date.now(), claimed: false },
    );
  }

  async function saveAccess(input: AccessSettings) {
    const saved = await putAccess(input);
    setAccess(saved);
  }

  return (
    <>
      <div class="app">
        <nav class="rail" aria-label="sections">
          <div class="rail-mark">AEGIS</div>
          <For each={RAIL}>
            {(item, index) => (
              <button
                type="button"
                data-testid={`tab-${item.id}`}
                class={tab() === item.id ? "rail-item active" : "rail-item"}
                aria-current={tab() === item.id ? "page" : undefined}
                onClick={() => setTab(item.id)}
              >
                <span class="rail-num">{String(index() + 1).padStart(2, "0")}</span>
                <span class="rail-label">{item.label}</span>
              </button>
            )}
          </For>
          <div class="rail-spacer" />
          <div class="rail-foot">dns sinkhole</div>
        </nav>

        <header class="cmdbar">
          <span class="view-name">{VIEW_NAMES[tab()]}</span>
          <button type="button" class="palette-trigger" data-testid="palette-trigger" onClick={() => openPalette()}>
            <span>Jump to a view or run an action</span>
            <kbd>CTRL-K</kbd>
          </button>
          <div class="cmdbar-status">
            <span class={`live-chip${live() ? "" : " off"}`} data-testid="live-chip">
              <span class="live-dot" />
              {live() ? "live" : "paused"}
            </span>
            <span class="status-pill" data-testid="status">
              <span class="dot" />
              <span class="status-text">
                {upstreams()[0] || "no upstream"}
                {upstreams().length > 1 ? ` +${upstreams().length - 1}` : ""} · {ruleCount()} rules
              </span>
            </span>
          </div>
        </header>

        <main class="canvas">
          <Show when={error()}>
            <p class="alert-line error-line" role="alert">
              Could not load the configuration: {error()}
            </p>
          </Show>
          <Show when={tab() === "dashboard"}>
            <Dashboard
              entries={log.entries()}
              threats={threats()}
              windowMinutes={windowMinutes()}
              onSetWindow={setWindowMinutes}
              onOpenLog={() => setTab("log")}
              onFilter={(filter) => {
                setLogFilter(filter);
                setTab("log");
              }}
            />
          </Show>
          <Show when={tab() === "constellation"}>
            <div class="view" data-testid="screen-constellation">
              <Graph
                profiles={profiles()}
                clients={clients()}
                rules={rules()}
                defaultProfile={defaultProfile()}
                upstreams={upstreams()}
                log={log}
                devices={seenDevices(discoveries(), observed(), clients())}
                onSaveClient={saveClient}
                onSaveProfile={saveProfile}
                onSetDefault={makeDefault}
                onClaimDevice={claimDevice}
                onDeleteClient={deleteClient}
              />
            </div>
          </Show>
          <Show when={tab() === "log"}>
            <QueryLog
              log={log}
              filter={logFilter()}
              onRuleAdded={() => reload("rules", "status")}
            />
          </Show>
          <Show when={tab() === "clients"}>
            <Clients
              clients={clients()}
              profiles={profiles()}
              discoveries={discoveries()}
              onClaimDiscovery={claimDiscovery}
              observed={observed()}
              onClaimObserved={claimObserved}
              onDismissDiscovery={async (mac) => {
                await removeDiscovery(mac);
                setDiscoveries((rows) => dropBy(rows, (row) => row.mac, mac));
              }}
              onSave={saveClient}
              onDelete={deleteClient}
            />
          </Show>
          <Show when={tab() === "profiles"}>
            <Profiles
              profiles={profiles()}
              defaultProfile={defaultProfile()}
              safesearch={safesearch()}
              onSave={saveProfile}
              onDelete={deleteProfile}
              onSetDefault={makeDefault}
              onSaveSafesearch={saveProfileSafesearch}
            />
          </Show>
          <Show when={tab() === "rules"}>
            <Rules
              rules={rules()}
              schedules={schedules().map((schedule) => schedule.name)}
              clients={clients().map((client) => client.name)}
              onCreate={addRule}
              onUpdate={changeRule}
              onDelete={deleteRule}
            />
          </Show>
          <Show when={tab() === "rewrites"}>
            <Rewrites rewrites={rewrites()} onSave={saveRewrite} onDelete={deleteRewrite} />
          </Show>
          <Show when={tab() === "services"}>
            <BlockedServices
              services={services()}
              serviceGroups={serviceGroups()}
              profileNames={profiles().map((profile) => profile.name)}
              clients={clients()}
              defaultProfile={defaultProfile()}
              schedules={schedules()}
              windows={windows()}
              rules={rules()}
              onSave={saveClientServices}
              onRefreshServices={refreshServiceCatalog}
              onSaveWindow={addWindow}
              onDeleteWindow={deleteServiceWindow}
              onSaveSchedule={addSchedule}
              onDeleteSchedule={deleteSchedule}
            />
          </Show>
          <Show when={tab() === "system"}>
            <div class="view">
              <div class="system-subnav" role="tablist" aria-label="system views">
                <For each={SYSTEM_VIEWS}>
                  {(view) => (
                    <button
                      type="button"
                      data-testid={`tab-${view.id}`}
                      class={systemView() === view.id ? "btn-mini active" : "btn-mini"}
                      aria-current={systemView() === view.id ? "page" : undefined}
                      onClick={() => setSystemView(view.id)}
                    >
                      {view.label}
                    </button>
                  )}
                </For>
              </div>
              <Show when={systemView() === "upstreams"}>
                <Upstreams
                  upstreams={upstreamRows()}
                  routes={routes()}
                  onSaveUpstream={saveUpstream}
                  onDeleteUpstream={deleteUpstream}
                  onCreateRoute={addRoute}
                  onUpdateRoute={changeRoute}
                  onDeleteRoute={deleteRoute}
                />
              </Show>
              <Show when={systemView() === "sources"}>
                <Sources
                  sources={sources()}
                  catalog={catalog()}
                  onSave={saveSource}
                  onDelete={deleteSource}
                  onReload={refresh}
                />
              </Show>
              <Show when={systemView() === "settings"}>
                <Settings
                  profiles={profiles()}
                  defaultProfile={defaultProfile()}
                  onSetDefault={makeDefault}
                  access={access()}
                  onSaveAccess={saveAccess}
                  windowMinutes={windowMinutes()}
                  onSetWindow={setWindowMinutes}
                  live={live()}
                  onSetLive={setLive}
                />
              </Show>
            </div>
          </Show>
        </main>

        <footer class="strip">
          <span>{rules().length} custom rules</span>
          <span class="sep">·</span>
          <span>{ruleCount()} list rules</span>
          <span class="sep">·</span>
          <span>{clients().length} clients</span>
          <span class="sep">·</span>
          <span>{profiles().length} profiles</span>
          <span class="sep">·</span>
          <span>{upstreams()[0] || "no upstream"}</span>
          <span class="sep">·</span>
          <span>aegis console</span>
        </footer>
      </div>

      <Show when={paletteOpen()}>
        <div class="palette-scrim" onClick={() => setPaletteOpen(false)} />
        <div class="palette" role="dialog" aria-modal="true" aria-label="command palette" data-testid="palette">
          <input
            data-testid="palette-input"
            placeholder="Type a view or action…"
            value={paletteQuery()}
            ref={(element) => setTimeout(() => element.focus(), 0)}
            onInput={(event) => {
              setPaletteQuery(event.currentTarget.value);
              setPaletteIndex(0);
            }}
            onKeyDown={onPaletteKey}
          />
          <ul class="palette-list">
            <For each={paletteItems()}>
              {(item, index) => (
                <li>
                  <button
                    type="button"
                    class={paletteIndex() === index() ? "active" : undefined}
                    onMouseEnter={() => setPaletteIndex(index())}
                    onClick={() => runPaletteItem(index())}
                  >
                    <span class="p-label">{item.label}</span>
                    <span class="p-hint">{item.hint}</span>
                  </button>
                </li>
              )}
            </For>
            <Show when={paletteItems().length === 0}>
              <li class="palette-empty">nothing matches</li>
            </Show>
          </ul>
        </div>
      </Show>
    </>
  );
}
